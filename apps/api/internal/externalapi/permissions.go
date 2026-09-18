package externalapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yldm-tech/pace/apps/api/internal/access"
	"github.com/yldm-tech/pace/apps/api/internal/auth"

	"gorm.io/gorm/clause"
)

// The three project roles, which are the same numbers the session API uses.
const (
	roleGuest  = 5
	roleMember = 15
	roleAdmin  = 20
)

// requireProjectMember is ProjectEntityPermission: a safe method wants any active member, and a write wants an admin or a member.
func (handler *Handler) requireProjectMember(c *gin.Context, user *auth.User, method string) bool {
	role, member, err := access.ProjectRole(c.Request.Context(), handler.db, c.Param("slug"), c.Param("project"), user.ID)
	if err != nil {
		handler.serverError(c, err)
		return false
	}
	if !member {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"detail": "You do not have permission to perform this action.",
		})
		return false
	}
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return true
	}
	if role == roleAdmin || role == roleMember {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"detail": "You do not have permission to perform this action.",
	})
	return false
}

func newUUID() (string, error) {
	identifier, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return identifier.String(), nil
}

// isUniqueViolation reports the one database error the external API turns into a conflict rather than a 500.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// onConflictDoNothing is the clause a bulk create with ignore_conflicts needs.
func onConflictDoNothing() clause.OnConflict {
	return clause.OnConflict{DoNothing: true}
}
