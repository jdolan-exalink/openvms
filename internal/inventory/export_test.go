package inventory

// SetPurgeBatchForTest changes how many due cameras one listing of the history purge returns, and
// returns a function that restores it.
func SetPurgeBatchForTest(n int32) (restore func()) {
	old := purgeBatch
	purgeBatch = n
	return func() { purgeBatch = old }
}
