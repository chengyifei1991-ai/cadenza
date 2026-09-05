// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestSPAHandler 验证静态资源与 SPA fallback 行为（History 路由可直达）。
func TestSPAHandler(t *testing.T) {
	root := fstest.MapFS{
		"index.html":      {Data: []byte("<div id=\"root\">cadenza</div>")},
		"assets/app.js":   {Data: []byte("console.log(1)")},
		"assets/logo.svg": {Data: []byte("<svg/>")},
	}
	h := SPAHandler(root)

	get := func(p string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	body := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		b, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("读取响应失败: %v", err)
		}
		return string(b)
	}

	cases := []struct {
		name   string
		path   string
		want   int
		substr string
	}{
		{name: "根路径返回 index", path: "/", want: 200, substr: "<div id=\"root\">"},
		{name: "静态文件原样返回", path: "/assets/app.js", want: 200, substr: "console.log"},
		{name: "子路径(前端路由)回退 index", path: "/collectors/uid-1", want: 200, substr: "<div id=\"root\">"},
		{name: "无后缀路由回退 index", path: "/app/foo", want: 200, substr: "<div id=\"root\">"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(tc.path)
			if rec.Code != tc.want {
				t.Fatalf("%s status = %d, want %d", tc.path, rec.Code, tc.want)
			}
			if !strings.Contains(body(rec), tc.substr) {
				t.Errorf("%s body 缺少 %q", tc.path, tc.substr)
			}
		})
	}

	// 静态子目录 301 语义不破坏；无 index 时返回 503。
	empty := SPAHandler(fstest.MapFS{})
	rec := getWith(empty, "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("无 index 时 status = %d, want 503", rec.Code)
	}
}

func getWith(h http.Handler, p string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
	return rec
}
