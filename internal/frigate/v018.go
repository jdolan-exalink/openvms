package frigate

// v018 implements Adapter for Frigate 0.18. The read API matches 0.17; exports are queued
// as jobs, so their state is read from /api/jobs/export until the file exists
// (see v018.Export).
type v018 struct {
	v017
}

func (a *v018) Name() string { return "v018" }
