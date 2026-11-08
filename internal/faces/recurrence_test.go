package faces

import (
	"errors"
	"strings"
	"testing"
)

func TestClusterRuleValidateAcceptsTheBoundsAndNamesTheFieldOutOfRange(t *testing.T) {
	for _, ok := range []ClusterRule{{MinAppearances: 1, WindowDays: 1}, {MinAppearances: 100, WindowDays: 365}, DefaultClusterRule} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, c := range []struct {
		rule  ClusterRule
		field string
	}{
		{ClusterRule{MinAppearances: 0, WindowDays: 7}, "min_appearances"},
		{ClusterRule{MinAppearances: 101, WindowDays: 7}, "min_appearances"},
		{ClusterRule{MinAppearances: 3, WindowDays: 0}, "window_days"},
		{ClusterRule{MinAppearances: 3, WindowDays: 366}, "window_days"},
	} {
		var v *ValidationError
		if err := c.rule.Validate(); !errors.As(err, &v) || !strings.Contains(v.Msg, c.field) {
			t.Errorf("%+v: err = %v, want a ValidationError naming %s", c.rule, err, c.field)
		}
	}
}
