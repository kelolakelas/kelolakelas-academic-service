package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const emailClaimTestSecret = "kel75-email-claim"

func signEmailClaimToken(t *testing.T, claims Claims) string {
	t.Helper()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(emailClaimTestSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// KEL-75: the parent's email claim must reach the handlers through the gin
// context set by the verified-token middleware. It is the only source for the
// billing contact of a parent checkout.
func TestAuthMiddlewareExposesEmailClaim(t *testing.T) {
	tests := []struct {
		name      string
		claim     string
		wantEmail string
	}{
		{name: "email claim is exposed", claim: "parent@example.com", wantEmail: "parent@example.com"},
		{name: "surrounding whitespace is trimmed once", claim: "  parent@example.com  ", wantEmail: "parent@example.com"},
		{name: "missing claim stays empty", claim: "", wantEmail: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var gotEmail string
			router.GET("/probe", AuthMiddleware(emailClaimTestSecret), func(c *gin.Context) {
				gotEmail = c.GetString("email")
				c.Status(http.StatusOK)
			})

			token := signEmailClaimToken(t, Claims{UserID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", Email: test.claim, IsParent: true})
			request := httptest.NewRequest(http.MethodGet, "/probe", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if gotEmail != test.wantEmail {
				t.Fatalf("context email = %q, want %q", gotEmail, test.wantEmail)
			}
		})
	}
}

// A request without a valid token must never reach a handler, email claim or not.
func TestAuthMiddlewareStillRejectsUnsignedEmailRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	reached := false
	router.GET("/probe", AuthMiddleware(emailClaimTestSecret), func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/probe", strings.NewReader(`{"email":"spoof@example.com"}`))
	request.Header.Set("Authorization", "Bearer not-a-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", recorder.Code)
	}
	if reached {
		t.Fatal("handler ran without a valid token")
	}
}
