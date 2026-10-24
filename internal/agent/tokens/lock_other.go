//go:build !unix

package tokens

// lockFile has nothing to lock where flock does not exist; the agent targets Linux hosts.
func lockFile(string) (func(), error) { return func() {}, nil }
