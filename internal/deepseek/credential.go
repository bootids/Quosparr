package deepseek

import "os"

// EnvKey returns the DeepSeek API key from DEEPSEEK_API_KEY. DeepSeek has no
// credentials file to discover; the env var is the only fallback after the
// explicit configuration.
func EnvKey() string {
	return os.Getenv("DEEPSEEK_API_KEY")
}
