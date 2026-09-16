package services

import (
	"os/exec"
	"strings"
	"testing"
)

// The session URL is read from the journal by a shell pipeline, so it is tested by running
// that pipeline on journal output captured from real instances — including the switch from
// sshx.io (0.4.1) to the in-house server (0.5.1) that a hardcoded host missed.
func TestSshxLinkExtract_ReadsTheLatestLinkWhateverTheHost(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	cases := []struct {
		name, journal, want string
	}{
		{
			name: "0.4.1 on sshx.io",
			journal: "Started SSHX collaborative terminal.\n" +
				"  \x1b[1msshx\x1b[0m v0.4.1\n" +
				"  \x1b[32m➜\x1b[0m  Link:  \x1b[4mhttps://sshx.io/s/9T0bktW1d4#IOfD9ItVilJ9MA\x1b[0m\n" +
				"  ➜  Shell: /bin/bash\n",
			want: "https://sshx.io/s/9T0bktW1d4#IOfD9ItVilJ9MA",
		},
		{
			name: "restart onto 0.5.1: the newest link wins",
			journal: "  sshx v0.4.1\n" +
				"  ➜  Link:  https://sshx.io/s/9T0bktW1d4#IOfD9ItVilJ9MA\n" +
				"Stopped SSHX collaborative terminal.\n" +
				"  sshx v0.5.1\n" +
				"  ➜  Link:  https://jla-dev.burro-piranha.ts.net/s/9VARIDDLpS#drcmFX15xMJfNl\n" +
				"2026-09-16T09:16:15.910733Z ERROR sshx::controller: disconnected, retrying in 1s... err=server closed connection\n",
			want: "https://jla-dev.burro-piranha.ts.net/s/9VARIDDLpS#drcmFX15xMJfNl",
		},
		{
			name:    "not started yet",
			journal: "Started SSHX collaborative terminal.\n",
			want:    "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", sshxLinkExtract)
			cmd.Stdin = strings.NewReader(c.journal)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("pipeline failed: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
