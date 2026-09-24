package api

import "testing"

func TestBuildLogStartsImageBuild(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{line: "Receiving objects: 42% (118/281), 12.00 MiB | 100.00 KiB/s", want: false},
		{line: "source revision ready", want: false},
		{line: "building and pushing image with BuildKit", want: true},
		{line: "#1 [internal] load build definition from Dockerfile", want: true},
	}

	for _, test := range tests {
		if got := buildLogStartsImageBuild(test.line); got != test.want {
			t.Errorf("buildLogStartsImageBuild(%q) = %v, want %v", test.line, got, test.want)
		}
	}
}

func TestBuildLogSource(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "preparing source workspace", want: "git"},
		{line: "Receiving objects: 42% (118/281), 12.00 MiB | 100.00 KiB/s", want: "git"},
		{line: "fatal: unable to access 'https://github.com/example/repo.git/': Couldn't connect to server", want: "git"},
		{line: "git network attempt 1/4 failed, retrying in 3s", want: "git"},
		{line: "f44688b464cbe4c3cf60612c68509e981e0081bd", want: "git"},
		{line: "#1 [internal] load build definition from Dockerfile", want: "build"},
		{line: "#5 pushing layers 0.1s done", want: "registry"},
		{line: "image=registry.local/app@sha256:1234", want: "registry"},
	}

	for _, test := range tests {
		if got := buildLogSource(test.line); got != test.want {
			t.Errorf("buildLogSource(%q) = %q, want %q", test.line, got, test.want)
		}
	}
}
