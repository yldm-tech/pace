package externalapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// currentUser answers with whoever the key belongs to.
//
// The external API's UserLiteSerializer is not the session API's: it carries the email, which the session one only reveals to an admin.
func (handler *Handler) currentUser(c *gin.Context, user *auth.User, _ APIToken) {
	drf.Respond(c, http.StatusOK, gin.H{
		"id": user.ID, "first_name": user.FirstName, "last_name": user.LastName,
		"email": user.Email, "avatar": user.Avatar, "avatar_url": avatarURL(user),
		"display_name": user.DisplayName,
	})
}

// avatarURL prefers the uploaded asset over the stored url, the way every avatar in the codebase is rendered.
func avatarURL(user *auth.User) any {
	if user.AvatarAssetID != nil {
		return "/api/assets/v2/static/" + *user.AvatarAssetID + "/"
	}
	if user.Avatar != "" {
		return user.Avatar
	}
	return nil
}
