package library

import (
	"crypto/rand"
	"encoding/base32"
	"regexp"
	"strings"
)

const maxSlugLength = 64

var (
	crockford  = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)
	nonSlug    = regexp.MustCompile(`[^a-z0-9]+`)
	strictSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func NewID() ID {
	var random [16]byte
	_, _ = rand.Read(random[:])
	return ID("ps_" + crockford.EncodeToString(random[:]))
}

func Slug(name string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > maxSlugLength {
		slug = strings.TrimRight(slug[:maxSlugLength], "-")
	}
	return slug
}
