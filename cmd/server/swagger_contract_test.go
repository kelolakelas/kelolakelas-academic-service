package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/docs"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
)

const contractInternalCredential = "swagger-contract-internal-credential"

type swaggerOperation struct {
	Security   []map[string][]string `json:"security"`
	Permission *struct {
		Permission   string `json:"permission"`
		ParentTokens string `json:"parent_tokens"`
	} `json:"x-permission"`
}

type swaggerDoc struct {
	Paths               map[string]map[string]swaggerOperation `json:"paths"`
	SecurityDefinitions map[string]struct {
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"securityDefinitions"`
	Definitions map[string]struct {
		Properties map[string]json.RawMessage `json:"properties"`
	} `json:"definitions"`
}

// contractEngine registers the production route table. Handlers are zero values:
// the contract tests only exercise middleware, and a request that reaches a handler
// is recovered as a 500 without touching any dependency.
func contractEngine(t *testing.T, permissions *routePermissionClient) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	registerRoutes(engine, routeHandlers{health: ok, ready: ok}, routeTestSecret, contractInternalCredential, permissions)
	return engine
}

func loadSwagger(t *testing.T) swaggerDoc {
	t.Helper()
	var doc swaggerDoc
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &doc); err != nil {
		t.Fatalf("docs.SwaggerInfo is not valid JSON: %v", err)
	}
	return doc
}

var ginParam = regexp.MustCompile(`:([A-Za-z_]+)`)

type contractRoute struct{ method, path string }

func (r contractRoute) String() string { return r.method + " " + r.path }

func registeredRoutes(engine *gin.Engine) []contractRoute {
	var routes []contractRoute
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/swagger/") {
			continue
		}
		routes = append(routes, contractRoute{strings.ToLower(route.Method), ginParam.ReplaceAllString(route.Path, "{$1}")})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].String() < routes[j].String() })
	return routes
}

var swaggerParam = regexp.MustCompile(`\{[A-Za-z_]+\}`)

func concretePath(path string) string {
	return swaggerParam.ReplaceAllStringFunc(path, func(string) string { return uuid.NewString() })
}

// publicRoutes answer without credentials. Everything under /internal takes the
// service credential; every other route takes a user JWT.
var publicRoutes = map[string]bool{
	"get /health":                      true,
	"get /ready":                       true,
	"get /api/v1/catalog/classes":      true,
	"get /api/v1/catalog/classes/{id}": true,
}

func expectedScheme(route contractRoute) string {
	switch {
	case publicRoutes[route.String()]:
		return ""
	case strings.HasPrefix(route.path, "/internal/"):
		return "InternalServiceCredential"
	default:
		return "BearerAuth"
	}
}

func TestSwaggerDocumentsEveryRegisteredRouteAndNothingElse(t *testing.T) {
	doc := loadSwagger(t)
	registered := map[string]bool{}
	for _, route := range registeredRoutes(contractEngine(t, &routePermissionClient{})) {
		registered[route.String()] = true
		if _, ok := doc.Paths[route.path][route.method]; !ok {
			t.Errorf("registered route %s is missing from docs/swagger (run make swagger)", route)
		}
	}
	for path, operations := range doc.Paths {
		for method := range operations {
			if !registered[method+" "+path] {
				t.Errorf("docs/swagger documents %s %s, which is not registered", method, path)
			}
		}
	}
}

func TestSwaggerSecurityMatchesMiddleware(t *testing.T) {
	doc := loadSwagger(t)
	if def := doc.SecurityDefinitions["BearerAuth"]; def.Name != "Authorization" || def.In != "header" {
		t.Errorf("BearerAuth definition = %+v", def)
	}
	if def := doc.SecurityDefinitions["InternalServiceCredential"]; def.Name != "X-Internal-Service-Credential" || def.In != "header" {
		t.Errorf("InternalServiceCredential definition = %+v", def)
	}
	engine := contractEngine(t, &routePermissionClient{})
	for _, route := range registeredRoutes(engine) {
		op := doc.Paths[route.path][route.method]
		var documented []string
		for _, requirement := range op.Security {
			for scheme := range requirement {
				documented = append(documented, scheme)
			}
		}
		want := expectedScheme(route)
		if (want == "" && len(documented) != 0) || (want != "" && (len(documented) != 1 || documented[0] != want)) {
			t.Errorf("%s documents security %v, want %q", route, documented, want)
		}

		// The documented scheme must be the one the middleware enforces: a request
		// without credentials is refused on every protected route and served on the
		// public ones.
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(strings.ToUpper(route.method), concretePath(route.path), nil))
		if want == "" && response.Code == http.StatusUnauthorized {
			t.Errorf("%s is documented as public but answered 401", route)
		}
		if want != "" && response.Code != http.StatusUnauthorized {
			t.Errorf("%s without credentials answered %d, want 401", route, response.Code)
		}
	}
}

func TestSwaggerPermissionNotesMatchMiddleware(t *testing.T) {
	doc := loadSwagger(t)
	member := routeToken(t, middleware.Claims{UserID: uuid.NewString(), TenantID: uuid.NewString(), RoleID: uuid.NewString(), MemberID: uuid.NewString()})
	parent := routeToken(t, middleware.Claims{UserID: uuid.NewString(), TenantID: uuid.NewString(), IsParent: true})
	checked := 0
	for _, route := range registeredRoutes(contractEngine(t, &routePermissionClient{})) {
		if expectedScheme(route) != "BearerAuth" {
			continue
		}
		note := doc.Paths[route.path][route.method].Permission

		// A client that denies everything records which permission the route asks for.
		denyAll := &routePermissionClient{}
		response := routeRequest(contractEngine(t, denyAll), strings.ToUpper(route.method), concretePath(route.path), "", member)
		if note == nil {
			if len(denyAll.calls) != 0 {
				t.Errorf("%s checks %v but documents no x-permission", route, denyAll.calls)
			}
			continue
		}
		checked++
		if len(denyAll.calls) != 1 || denyAll.calls[0] != note.Permission || response.Code != http.StatusForbidden {
			t.Errorf("%s documents %q; member request checked %v and answered %d", route, note.Permission, denyAll.calls, response.Code)
		}

		parentClient := &routePermissionClient{}
		response = routeRequest(contractEngine(t, parentClient), strings.ToUpper(route.method), concretePath(route.path), "", parent)
		switch note.ParentTokens {
		case "denied":
			if response.Code != http.StatusForbidden || len(parentClient.calls) != 0 {
				t.Errorf("%s documents parent tokens denied; got %d with checks %v", route, response.Code, parentClient.calls)
			}
		case "skipped":
			if response.Code == http.StatusForbidden || len(parentClient.calls) != 0 {
				t.Errorf("%s documents parent tokens skipped; got %d with checks %v", route, response.Code, parentClient.calls)
			}
		default:
			t.Errorf("%s has unknown parent_tokens %q", route, note.ParentTokens)
		}
	}
	if checked == 0 {
		t.Fatal("no x-permission notes found; the swagger contract lost its permission annotations")
	}
}

func TestSwaggerStudentSchemaUsesLastName(t *testing.T) {
	doc := loadSwagger(t)
	student, ok := doc.Definitions["github_com_kelolakelas_kelolakelas-academic-service_internal_domain.Student"]
	if !ok {
		t.Fatal("docs/swagger has no domain.Student definition")
	}
	if _, ok := student.Properties["last_name"]; !ok {
		t.Errorf("domain.Student properties lack last_name: %v", keys(student.Properties))
	}
	if strings.Contains(docs.SwaggerInfo.ReadDoc(), "last\u00e5_name") {
		t.Error("docs/swagger still contains the legacy last\u00e5_name key")
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
