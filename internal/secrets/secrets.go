package secrets

import "regexp"

const (
	envScheme        = "env:"
	keychainScheme   = "keychain:"
	needsSetupScheme = "needs-setup:"

	envName = `[A-Za-z_][A-Za-z0-9_]*`

	envSection     = "env"
	headersSection = "headers"
	argsSection    = "args"
	commandKey     = "command"
	urlKey         = "url"
)

var (
	secretName           = regexp.MustCompile(`(?i)(key|token|secret|password|passwd|auth|bearer|cookie|credential|session)`)
	validEnvName         = regexp.MustCompile(`^` + envName + `$`)
	canonicalPlaceholder = regexp.MustCompile(`\$\{env:(` + envName + `)\}`)
)

func IsSecretName(name string) bool { return secretName.MatchString(name) }
