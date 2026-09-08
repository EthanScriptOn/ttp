# TTP A/B Router for Apache APISIX

`ttp-ab-router.lua` is the gateway-side part of TTP's JSON request routing. It
handles ordinary HTTP requests; no JWT is required. The rule is declarative:
the plugin reads a scalar value from the JSON body, hashes it together with the
experiment ID, and sends the request to the A or B APISIX upstream.

## Request behavior

For a rule with `routing_rule.path = "$.wx_id"`:

```json
{"wx_id":"wx_123456","message":"hello"}
```

The same `wx_id` remains on the same variant for this experiment. A missing,
malformed, oversized, or non-scalar field is sent to A. The plugin overwrites
`X-TTP-AB-Experiment` and `X-TTP-AB-Variant` after it makes the decision; a
client cannot select B by supplying those headers.

Supported paths are field and numeric array-index paths, for example:

```text
$.wx_id
$.user.openid
$.items[0].tenant.id
```

The plugin does not execute Lua supplied by a user. TTP validates the same path
grammar before it stores the experiment.

## APISIX route configuration

Create two APISIX upstreams for the already deployed A and B workloads, then
attach the plugin to the route that receives the application's HTTP traffic:

```json
{
  "uri": "/api/*",
  "upstream_id": "ab-fallback-upstream",
  "plugins": {
    "ttp-ab-router": {
      "experiment_id": "ab-7f31a2c4",
      "assignment": "user_id",
      "a_upstream_id": "checkout-a",
      "b_upstream_id": "checkout-b",
      "a_traffic": 99,
      "b_traffic": 1,
      "routing_rule": {
        "source": "json_body",
        "path": "$.wx_id",
        "missing_behavior": "stable",
        "algorithm": "consistent_hash"
      }
    }
  }
}
```

For an experiment configured as `percentage`, the plugin hashes the APISIX
request ID instead of reading the body. `user_id` is retained as the API value
for compatibility, but it means “fixed grouping by the configured JSON field”.

Load the plugin through APISIX's normal custom-plugin configuration, for
example by adding its directory to `extra_lua_path` and `ttp-ab-router` to the
custom plugin list. The exact Helm or standalone configuration depends on how
APISIX is installed.

TTP stores and returns the rule when an experiment is created. The current
Kubernetes provider still does not automatically create APISIX routes or
upstreams; those resources must be provisioned by the cluster's gateway
configuration until that provider is added.
