package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"testing"
)

// helper to create a minimal ResolvedRoute for testing
func makeRoute(method, path string) *ResolvedRoute {
	u, _ := url.Parse("http://localhost:8080")
	return &ResolvedRoute{
		Method:       method,
		PathPattern:  path,
		TargetURL:    u,
		ProxyHandler: httputil.NewSingleHostReverseProxy(u),
	}
}

func TestRadixTree_StaticRoutes(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users", makeRoute("GET", "/api/users"))
	tree.Insert("GET", "/api/products", makeRoute("GET", "/api/products"))
	tree.Insert("POST", "/api/users", makeRoute("POST", "/api/users"))
	tree.Insert("GET", "/health", makeRoute("GET", "/health"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantMatch  bool
		wantPath   string
	}{
		{"GET /api/users", "GET", "/api/users", true, "/api/users"},
		{"POST /api/users", "POST", "/api/users", true, "/api/users"},
		{"GET /api/products", "GET", "/api/products", true, "/api/products"},
		{"GET /health", "GET", "/health", true, "/health"},
		{"DELETE /api/users - not found", "DELETE", "/api/users", false, ""},
		{"GET /api/unknown - not found", "GET", "/api/unknown", false, ""},
		{"GET / - not found", "GET", "/", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, _ := tree.Search(tt.method, tt.path)
			if tt.wantMatch {
				if route == nil {
					t.Fatalf("expected match for %s %s, got nil", tt.method, tt.path)
				}
				if route.PathPattern != tt.wantPath {
					t.Errorf("expected PathPattern=%q, got %q", tt.wantPath, route.PathPattern)
				}
			} else {
				if route != nil {
					t.Fatalf("expected no match for %s %s, got %+v", tt.method, tt.path, route)
				}
			}
		})
	}
}

func TestRadixTree_ParameterizedRoutes(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))
	tree.Insert("GET", "/api/users/:id/posts", makeRoute("GET", "/api/users/:id/posts"))
	tree.Insert("PUT", "/api/users/:id", makeRoute("PUT", "/api/users/:id"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantMatch  bool
		wantPath   string
		wantParams map[string]string
	}{
		{
			"GET /api/users/123",
			"GET", "/api/users/123", true, "/api/users/:id",
			map[string]string{"id": "123"},
		},
		{
			"PUT /api/users/456",
			"PUT", "/api/users/456", true, "/api/users/:id",
			map[string]string{"id": "456"},
		},
		{
			"GET /api/users/789/posts",
			"GET", "/api/users/789/posts", true, "/api/users/:id/posts",
			map[string]string{"id": "789"},
		},
		{
			"DELETE /api/users/123 - method not found",
			"DELETE", "/api/users/123", false, "", nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, params := tree.Search(tt.method, tt.path)
			if tt.wantMatch {
				if route == nil {
					t.Fatalf("expected match for %s %s, got nil", tt.method, tt.path)
				}
				if route.PathPattern != tt.wantPath {
					t.Errorf("expected PathPattern=%q, got %q", tt.wantPath, route.PathPattern)
				}
				for k, v := range tt.wantParams {
					if params[k] != v {
						t.Errorf("expected param %s=%q, got %q", k, v, params[k])
					}
				}
			} else {
				if route != nil {
					t.Fatalf("expected no match for %s %s, got %+v", tt.method, tt.path, route)
				}
			}
		})
	}
}

func TestRadixTree_StaticPriorityOverParam(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users/admin", makeRoute("GET", "/api/users/admin"))
	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))

	// Static route "admin" should take priority over param ":id"
	route, params := tree.Search("GET", "/api/users/admin")
	if route == nil {
		t.Fatal("expected match for GET /api/users/admin")
	}
	if route.PathPattern != "/api/users/admin" {
		t.Errorf("expected static route /api/users/admin, got %s", route.PathPattern)
	}
	if len(params) > 0 {
		t.Errorf("expected no params for static match, got %v", params)
	}

	// Non-admin should match the param route
	route, params = tree.Search("GET", "/api/users/99")
	if route == nil {
		t.Fatal("expected match for GET /api/users/99")
	}
	if route.PathPattern != "/api/users/:id" {
		t.Errorf("expected param route /api/users/:id, got %s", route.PathPattern)
	}
	if params["id"] != "99" {
		t.Errorf("expected param id=99, got %s", params["id"])
	}
}

func TestRadixTree_StaticPriorityOverParam_ReverseInsert(t *testing.T) {
	tree := NewRadixTree()

	// Insert param first, then static — static should still win
	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))
	tree.Insert("GET", "/api/users/admin", makeRoute("GET", "/api/users/admin"))

	route, _ := tree.Search("GET", "/api/users/admin")
	if route == nil {
		t.Fatal("expected match for GET /api/users/admin")
	}
	if route.PathPattern != "/api/users/admin" {
		t.Errorf("expected static route /api/users/admin, got %s", route.PathPattern)
	}
}

func TestRadixTree_MultipleParams(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users/:userId/posts/:postId", makeRoute("GET", "/api/users/:userId/posts/:postId"))

	route, params := tree.Search("GET", "/api/users/42/posts/100")
	if route == nil {
		t.Fatal("expected match")
	}
	if params["userId"] != "42" {
		t.Errorf("expected userId=42, got %s", params["userId"])
	}
	if params["postId"] != "100" {
		t.Errorf("expected postId=100, got %s", params["postId"])
	}
}

func TestRadixTree_OverlappingPrefixes(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users", makeRoute("GET", "/api/users"))
	tree.Insert("GET", "/api/users/search", makeRoute("GET", "/api/users/search"))
	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))
	tree.Insert("GET", "/api", makeRoute("GET", "/api"))

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{"exact /api", "/api", "/api"},
		{"exact /api/users", "/api/users", "/api/users"},
		{"exact /api/users/search", "/api/users/search", "/api/users/search"},
		{"param /api/users/42", "/api/users/42", "/api/users/:id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, _ := tree.Search("GET", tt.path)
			if route == nil {
				t.Fatalf("expected match for GET %s", tt.path)
			}
			if route.PathPattern != tt.wantPath {
				t.Errorf("expected PathPattern=%q, got %q", tt.wantPath, route.PathPattern)
			}
		})
	}
}

func TestRadixTree_RootPath(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/", makeRoute("GET", "/"))

	route, _ := tree.Search("GET", "/")
	if route == nil {
		t.Fatal("expected match for GET /")
	}
	if route.PathPattern != "/" {
		t.Errorf("expected PathPattern=%q, got %q", "/", route.PathPattern)
	}
}

func TestGetPathParams(t *testing.T) {
	r, _ := http.NewRequest("GET", "/test", nil)

	// No params set
	params := GetPathParams(r)
	if params != nil {
		t.Errorf("expected nil params, got %v", params)
	}
}

// === Benchmarks ===

func BenchmarkRadixTree_Search_Static(b *testing.B) {
	tree := NewRadixTree()
	paths := []string{
		"/api/users", "/api/products", "/api/orders", "/api/invoices",
		"/api/customers", "/api/reports", "/api/analytics", "/api/settings",
		"/api/dashboard", "/api/notifications", "/health", "/ready",
	}
	for _, p := range paths {
		tree.Insert("GET", p, makeRoute("GET", p))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Search("GET", "/api/analytics")
	}
}

func BenchmarkRadixTree_Search_Parameterized(b *testing.B) {
	tree := NewRadixTree()
	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))
	tree.Insert("GET", "/api/users/:id/posts/:postId", makeRoute("GET", "/api/users/:id/posts/:postId"))
	tree.Insert("GET", "/api/products/:id", makeRoute("GET", "/api/products/:id"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Search("GET", "/api/users/12345/posts/67890")
	}
}

func BenchmarkRadixTree_Search_ManyRoutes(b *testing.B) {
	tree := NewRadixTree()

	// Simulate a gateway with many routes
	prefixes := []string{"users", "products", "orders", "customers", "invoices", "reports", "analytics", "settings", "dashboard", "notifications"}
	methods := []string{"GET", "POST", "PUT", "DELETE"}

	for _, prefix := range prefixes {
		for _, method := range methods {
			path := "/api/v1/" + prefix
			tree.Insert(method, path, makeRoute(method, path))
			pathWithId := "/api/v1/" + prefix + "/:id"
			tree.Insert(method, pathWithId, makeRoute(method, pathWithId))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Search("GET", "/api/v1/notifications/abc123")
	}
}

func BenchmarkRadixTree_Search_Wildcard(b *testing.B) {
	tree := NewRadixTree()
	tree.Insert("GET", "/static/*filepath", makeRoute("GET", "/static/*filepath"))
	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Search("GET", "/static/css/main.css")
	}
}

// === Wildcard Tests ===

func TestRadixTree_WildcardCatchAll(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/static/*filepath", makeRoute("GET", "/static/*filepath"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantMatch  bool
		wantPath   string
		wantParams map[string]string
	}{
		{
			"single file",
			"GET", "/static/main.css", true, "/static/*filepath",
			map[string]string{"filepath": "main.css"},
		},
		{
			"nested path",
			"GET", "/static/css/main.css", true, "/static/*filepath",
			map[string]string{"filepath": "css/main.css"},
		},
		{
			"deeply nested",
			"GET", "/static/assets/images/logo.png", true, "/static/*filepath",
			map[string]string{"filepath": "assets/images/logo.png"},
		},
		{
			"wrong method",
			"POST", "/static/main.css", false, "", nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, params := tree.Search(tt.method, tt.path)
			if tt.wantMatch {
				if route == nil {
					t.Fatalf("expected match for %s %s, got nil", tt.method, tt.path)
				}
				if route.PathPattern != tt.wantPath {
					t.Errorf("expected PathPattern=%q, got %q", tt.wantPath, route.PathPattern)
				}
				for k, v := range tt.wantParams {
					if params[k] != v {
						t.Errorf("expected param %s=%q, got %q", k, v, params[k])
					}
				}
			} else {
				if route != nil {
					t.Fatalf("expected no match for %s %s, got %+v", tt.method, tt.path, route)
				}
			}
		})
	}
}

func TestRadixTree_StaticPriorityOverWildcard(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/static/*filepath", makeRoute("GET", "/static/*filepath"))
	tree.Insert("GET", "/static/favicon.ico", makeRoute("GET", "/static/favicon.ico"))

	// Static route should take priority
	route, params := tree.Search("GET", "/static/favicon.ico")
	if route == nil {
		t.Fatal("expected match")
	}
	if route.PathPattern != "/static/favicon.ico" {
		t.Errorf("expected static route, got %s", route.PathPattern)
	}
	if len(params) > 0 {
		t.Errorf("expected no params for static match, got %v", params)
	}

	// Other paths should hit the wildcard
	route, params = tree.Search("GET", "/static/other.js")
	if route == nil {
		t.Fatal("expected match")
	}
	if route.PathPattern != "/static/*filepath" {
		t.Errorf("expected wildcard route, got %s", route.PathPattern)
	}
	if params["filepath"] != "other.js" {
		t.Errorf("expected filepath=other.js, got %s", params["filepath"])
	}
}

func TestRadixTree_ParamAndWildcard(t *testing.T) {
	tree := NewRadixTree()

	tree.Insert("GET", "/api/users/:id", makeRoute("GET", "/api/users/:id"))
	tree.Insert("GET", "/files/*path", makeRoute("GET", "/files/*path"))

	// Param route
	route, params := tree.Search("GET", "/api/users/42")
	if route == nil {
		t.Fatal("expected match")
	}
	if route.PathPattern != "/api/users/:id" {
		t.Errorf("expected param route, got %s", route.PathPattern)
	}
	if params["id"] != "42" {
		t.Errorf("expected id=42, got %s", params["id"])
	}

	// Wildcard route
	route, params = tree.Search("GET", "/files/docs/readme.md")
	if route == nil {
		t.Fatal("expected match")
	}
	if route.PathPattern != "/files/*path" {
		t.Errorf("expected wildcard route, got %s", route.PathPattern)
	}
	if params["path"] != "docs/readme.md" {
		t.Errorf("expected path=docs/readme.md, got %s", params["path"])
	}
}

func TestRadixTree_DuplicateRouteRejection(t *testing.T) {
	tree := NewRadixTree()

	// First insert should succeed
	err := tree.Insert("GET", "/api/users", makeRoute("GET", "/api/users"))
	if err != nil {
		t.Fatalf("first insert should succeed, got: %v", err)
	}

	// Duplicate method+path should be rejected
	err = tree.Insert("GET", "/api/users", makeRoute("GET", "/api/users"))
	if err == nil {
		t.Fatal("expected error for duplicate GET /api/users")
	}

	// Different method on same path should succeed
	err = tree.Insert("POST", "/api/users", makeRoute("POST", "/api/users"))
	if err != nil {
		t.Fatalf("POST on same path should succeed, got: %v", err)
	}

	// Different path with same method should succeed
	err = tree.Insert("GET", "/api/products", makeRoute("GET", "/api/products"))
	if err != nil {
		t.Fatalf("GET on different path should succeed, got: %v", err)
	}
}
