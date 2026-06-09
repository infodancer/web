package handler

import (
	"context"
	"html/template"
)

// Card renders the embeddable avatar+name unit for one user, for other
// components and host surfaces (forum posts, rosters, author bylines) to
// inline. The returned HTML is safe by construction: every dynamic value
// passes through html/template escaping. Returns store.ErrNotFound when the
// user has no profile row.
func (s *Server) Card(ctx context.Context, userKey string) (template.HTML, error) {
	p, err := s.store.Get(ctx, userKey)
	if err != nil {
		return "", err
	}
	v := cardView{
		BasePath:    s.basePath,
		Handle:      p.Handle,
		DisplayName: displayLabel(p),
		AvatarSrc:   s.avatarSrc(p),
	}
	if p.Handle != "" {
		v.ProfileURL = s.basePath + "/" + p.Handle
	}
	out, err := s.templates.Execute("partials/card.gohtml", v)
	if err != nil {
		return "", err
	}
	return template.HTML(out), nil //nolint:gosec // html/template-escaped fragment
}
