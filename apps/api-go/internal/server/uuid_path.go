// Copyright (c) 2023-present Plane Software, Inc. and contributors
// SPDX-License-Identifier: AGPL-3.0-only
// See the LICENSE file for details.

package server

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// uuidPathParameters are the route parameters Django declares with its uuid converter, named as this router names them.
//
// A converter is a route matcher, not a validator: <uuid:project_id> simply does not match a segment that is not a uuid, so Django's resolver answers 404 and the view never runs. Gin matches on position alone, so the segment reached the handler, went into a query, and Postgres ended the request with `invalid input syntax for type uuid` and a 500. Any address bar with a typo in it could produce one.
//
// The absentees are deliberate, and each was checked rather than assumed:
//
//   - slug, anchor, key, identifier and reaction are <str:> in Django.
//   - token is two different things: a password reset token in the auth routes, and an api token's id in the user routes. Only the second is a uuid.
//   - workspace is a uuid in most routes but a path component of an asset key in the legacy asset route, where it is concatenated with key rather than compared to a column.
//   - reference is a search term.
var uuidPathParameters = map[string]bool{
	"activity": true, "asset": true, "board": true, "comment": true, "cycle": true,
	"draft": true, "estimate": true, "favorite": true, "id": true, "intake": true,
	"invite": true, "issue": true, "label": true, "link": true, "member": true,
	"module": true, "notification": true, "page": true, "pk": true, "point": true,
	"project": true, "state": true, "sticky": true, "subscriber": true, "user": true,
	"version": true, "view": true, "webhook": true,
}

// requireUUIDPathParameters is Django's uuid converter: a malformed identifier is a route that does not exist.
func requireUUIDPathParameters() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, parameter := range c.Params {
			if !uuidPathParameters[parameter.Key] {
				continue
			}
			if _, err := uuid.Parse(parameter.Value); err != nil {
				// notFound rather than c.JSON, because this is the same 404 a path that matches nothing gets and it has to be the same bytes: Django writes that object with a space after the colon, which c.JSON does not.
				notFound(c)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
