package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func InternalServiceAuth(credential string) gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := sha256.Sum256([]byte(credential))
		provided := sha256.Sum256([]byte(c.GetHeader("X-Internal-Service-Credential")))
		if credential == "" || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid internal service credential", "data": nil})
			c.Abort()
			return
		}
		c.Next()
	}
}

type Claims struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	TenantID string `json:"tenant_id"`
	RoleID   string `json:"role_id"`
	MemberID string `json:"member_id"`
	IsParent bool   `json:"is_parent"`
	jwt.RegisteredClaims
}

func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid Authorization header", "data": nil})
			c.Abort()
			return
		}
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid || claims.UserID == "" || (!claims.IsParent && claims.TenantID == "") {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid or expired token", "data": nil})
			c.Abort()
			return
		}
		// The email claim is the billing contact for parent checkouts (KEL-75). It is
		// normalised here, once, at the only boundary that reads the verified token:
		// surrounding whitespace is stripped, the case is preserved. Handlers must take
		// the email from this context and never from the request body or headers.
		c.Set("user_id", claims.UserID)
		c.Set("email", strings.TrimSpace(claims.Email))
		c.Set("tenant_id", claims.TenantID)
		c.Set("role_id", claims.RoleID)
		c.Set("member_id", claims.MemberID)
		c.Set("is_parent", claims.IsParent)
		c.Next()
	}
}
