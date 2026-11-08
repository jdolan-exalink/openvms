package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/faces"
)

func TestClusterRuleForKeepsTheSavedRuleUnlessTheRequestNamesItsOwn(t *testing.T) {
	saved := faces.ClusterRule{MinAppearances: 5, WindowDays: 14}
	two, thirty := 2, 30
	for name, c := range map[string]struct {
		min, window *int
		want        faces.ClusterRule
	}{
		"nothing named":    {nil, nil, saved},
		"only appearances": {&two, nil, faces.ClusterRule{MinAppearances: 2, WindowDays: 14}},
		"only the window":  {nil, &thirty, faces.ClusterRule{MinAppearances: 5, WindowDays: 30}},
		"both named":       {&two, &thirty, faces.ClusterRule{MinAppearances: 2, WindowDays: 30}},
	} {
		if got := clusterRuleFor(saved, c.min, c.window); got != c.want {
			t.Errorf("%s: got %+v, want %+v", name, got, c.want)
		}
	}
}
