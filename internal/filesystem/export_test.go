package filesystem

// ResetRegistry clears the process-wide detector registry. Test-only: keeps
// registry_test.go's cases hermetic instead of order-dependent.
func ResetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	detectors = nil
}
