package qiniu

import "testing"

func TestGetBuildId(t *testing.T) {
	type tc struct {
		dir      string
		expected string
	}

	tcs := []tc{
		{dir: "logs/kodo-periodics-integration-test/1181915661132107776/", expected: "1181915661132107776"},
		{dir: "logs/kodo-periodics-integration-test/1181915661132107776", expected: ""},
		{dir: "pr-logs/directory/WIP-qtest-pull-request-kodo-test/1181915661132107776/", expected: "1181915661132107776"},
		{dir: "pr-logs/directory/WIP-qtest-pull-request-kodo-test/1181915661132107776.txt", expected: ""},
	}

	for _, tc := range tcs {
		got := getBuildId(tc.dir)
		if tc.expected != got {
			t.Errorf("getBuildId error, dir: %s, expect: %s, but got: %s", tc.dir, tc.expected, got)
		}
	}
}
