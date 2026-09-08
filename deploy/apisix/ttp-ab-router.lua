-- TTP A/B router for Apache APISIX.
-- The plugin reads a scalar value from a JSON request body and selects one of
-- two APISIX upstreams with a stable hash. It never trusts client TTP headers.
local core = require("apisix.core")

local plugin_name = "ttp-ab-router"
local max_body_bytes = 1024 * 1024

local schema = {
    type = "object",
    required = { "experiment_id", "a_upstream_id", "b_upstream_id" },
    properties = {
        experiment_id = { type = "string", minLength = 1, maxLength = 128 },
        assignment = { type = "string", enum = { "percentage", "user_id", "json_field" }, default = "percentage" },
        a_upstream_id = { type = "string", minLength = 1, maxLength = 128 },
        b_upstream_id = { type = "string", minLength = 1, maxLength = 128 },
        a_traffic = { type = "integer", minimum = 0, maximum = 100, default = 99 },
        b_traffic = { type = "integer", minimum = 0, maximum = 100, default = 1 },
        routing_rule = {
            type = "object",
            properties = {
                source = { type = "string", enum = { "json_body" }, default = "json_body" },
                path = { type = "string", minLength = 3, maxLength = 256 },
                missing_behavior = { type = "string", enum = { "stable" }, default = "stable" },
                algorithm = { type = "string", enum = { "consistent_hash" }, default = "consistent_hash" },
            },
        },
    },
}

local function parse_path(path)
    if type(path) ~= "string" or path:sub(1, 2) ~= "$." then
        return nil, "path must start with $."
    end

    local rest = path:sub(3)
    local first, tail = rest:match("^([A-Za-z_][A-Za-z0-9_]*)(.*)$")
    if not first then
        return nil, "path must contain a field name"
    end

    local tokens = { { field = first } }
    rest = tail
    while rest ~= "" do
        local field, field_tail = rest:match("^%.([A-Za-z_][A-Za-z0-9_]*)(.*)$")
        if field then
            table.insert(tokens, { field = field })
            rest = field_tail
        else
            local index, index_tail = rest:match("^%[(%d+)%](.*)$")
            if not index then
                return nil, "path contains an unsupported token"
            end
            table.insert(tokens, { index = tonumber(index) + 1 })
            rest = index_tail
        end
    end
    return tokens
end

local function read_request_body()
    ngx.req.read_body()
    local body = ngx.req.get_body_data()
    if not body then
        local body_file = ngx.req.get_body_file()
        if not body_file then
            return nil, "empty body"
        end
        local file, err = io.open(body_file, "rb")
        if not file then
            return nil, err or "cannot read request body"
        end
        body = file:read(max_body_bytes + 1)
        file:close()
    end
    if not body or #body == 0 then
        return nil, "empty body"
    end
    if #body > max_body_bytes then
        return nil, "request body is too large"
    end
    return body
end

local function read_json_value(path)
    local body, body_err = read_request_body()
    if not body then
        return nil, false, body_err
    end
    local document, decode_err = core.json.decode(body)
    if not document then
        return nil, false, decode_err or "invalid JSON body"
    end

    local tokens, path_err = parse_path(path)
    if not tokens then
        return nil, false, path_err
    end
    local current = document
    for _, token in ipairs(tokens) do
        if type(current) ~= "table" then
            return nil, false, "path does not resolve to a value"
        end
        current = current[token.field or token.index]
        if current == nil then
            return nil, false, "field is missing"
        end
    end
    local value_type = type(current)
    if value_type ~= "string" and value_type ~= "number" and value_type ~= "boolean" then
        return nil, false, "field must be a scalar"
    end
    return tostring(current), true
end

local function stable_bucket(value, experiment_id)
    return ngx.crc32_long(experiment_id .. "\0" .. value) % 100
end

local function choose_variant(conf, ctx)
    local assignment = conf.assignment or "percentage"
    if assignment == "json_field" then
        assignment = "user_id"
    end

    local b_traffic = conf.b_traffic or 1
    if assignment == "user_id" then
        local rule = conf.routing_rule or {}
        local value, present, value_err = read_json_value(rule.path or "$.user_id")
        if not present then
            core.log.warn(plugin_name, ": request field was not usable; route to A (", value_err or "unknown", ")")
            return "a"
        end
        return stable_bucket(value, conf.experiment_id) < b_traffic and "b" or "a"
    end

    local request_id = ngx.var.request_id or (ngx.var.remote_addr .. ":" .. ngx.var.request_uri)
    return stable_bucket(request_id, conf.experiment_id) < b_traffic and "b" or "a"
end

local _M = {
    version = 1.0,
    priority = 2500,
    name = plugin_name,
    schema = schema,
}

function _M.check_schema(conf)
    local ok, err = core.schema.check(schema, conf)
    if not ok then
        return false, err
    end
    if (conf.a_traffic or 99) + (conf.b_traffic or 1) ~= 100 then
        return false, "a_traffic and b_traffic must total 100"
    end
    if conf.assignment == "user_id" or conf.assignment == "json_field" then
        local rule = conf.routing_rule or {}
        if rule.source and rule.source ~= "json_body" then
            return false, "routing_rule.source must be json_body"
        end
        if not rule.path then
            return false, "routing_rule.path is required for JSON field assignment"
        end
        local _, path_err = parse_path(rule.path)
        if path_err then
            return false, path_err
        end
    end
    return true
end

function _M.access(conf, ctx)
    local variant = choose_variant(conf, ctx)
    local upstream_id = variant == "b" and conf.b_upstream_id or conf.a_upstream_id
    ctx.upstream_id = upstream_id

    -- Overwrite any incoming value. These headers are internal routing output,
    -- not a client-controlled way to select a variant.
    core.request.set_header(ctx, "X-TTP-AB-Experiment", conf.experiment_id)
    core.request.set_header(ctx, "X-TTP-AB-Variant", variant)
end

return _M
