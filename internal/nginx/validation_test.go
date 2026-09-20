package nginx

import (
	"nginx-builder/internal/model"
	"reflect"
	"testing"
)

func TestRejectUnknownAndInvalidInputs(t *testing.T) {
	cases := []struct {
		ids   []string
		paths map[string]string
		deps  *model.ThirdPartySourcesSpec
	}{
		{ids: []string{"unknown_module"}}, {ids: []string{""}},
		{paths: map[string]string{"typo": "/etc/nginx"}},
		{paths: map[string]string{"prefix": "/tmp/a;id"}},
		{paths: map[string]string{"prefix": ""}},
		{paths: map[string]string{"user": "root/nobody"}},
		{deps: &model.ThirdPartySourcesSpec{OpenSSLOpt: "no-shared;id"}},
		{deps: &model.ThirdPartySourcesSpec{UseZlibSource: true, ZlibVersion: "custom", ZlibSourceURL: "file:///tmp/source"}},
	}
	for i, c := range cases {
		if _, _, _, err := ValidateAndBuildArgs(c.ids, c.paths, c.deps, nil, true); err == nil {
			t.Fatalf("case %d accepted invalid input", i)
		}
	}
}
func TestArgumentsSortedAndDependencyClosure(t *testing.T) {
	paths := map[string]string{"prefix": " /opt/nginx ", "conf-path": "/etc/nginx/nginx.conf", "pid-path": "/run/nginx.pid", "error-log-path": "/var/log/nginx/error.log"}
	var expected []string
	for i := 0; i < 30; i++ {
		args, _, _, err := ValidateAndBuildArgs([]string{"without_http", "http_ssl", "without_pcre", "stream_ssl"}, paths, nil, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			expected = args
		} else if !reflect.DeepEqual(args, expected) {
			t.Fatal("nondeterministic arguments")
		}
		flags := map[string]bool{}
		for _, arg := range args {
			flags[arg] = true
		}
		if !flags["--prefix=/opt/nginx"] || !flags["--with-stream"] || !flags["--without-http_rewrite_module"] {
			t.Fatalf("dependency closure or normalized path missing: %v", args)
		}
	}
}
func TestResolvedSourcePathsAreAbsolute(t *testing.T) {
	args, _, _, err := ValidateAndBuildArgs(nil, nil, &model.ThirdPartySourcesSpec{UseZlibSource: true, ZlibVersion: "1.3.1"}, map[string]string{"zlib": "/var/build/deps/zlib-1.3.1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if arg == "--with-zlib=/var/build/deps/zlib-1.3.1" {
			return
		}
	}
	t.Fatal(args)
}
