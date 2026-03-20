package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	apierrors "github.com/calliope/api/pkg/errors"
)

const ContextKeyUserID = "user_id"

type jwtClaims struct {
	UserID uint64 `json:"uid"`
	jwt.RegisteredClaims
}

// Auth returns a Gin middleware that validates the JWT in the Authorization header
// and writes the user ID into the Gin context under ContextKeyUserID.
func Auth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			_ = c.Error(apierrors.ErrUnauthorized)
			c.Abort()
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			_ = c.Error(apierrors.ErrUnauthorized)
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*jwtClaims)
		if !ok || claims.UserID == 0 {
			_ = c.Error(apierrors.ErrUnauthorized)
			c.Abort()
			return
		}

		c.Set(ContextKeyUserID, claims.UserID)
		c.Next()
	}
}
