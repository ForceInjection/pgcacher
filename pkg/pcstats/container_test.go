//go:build linux

package pcstats

import "testing"

func TestValidateContainerID(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid 12 hex", "abc123def456", false},
		{"valid 64 hex", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", false},
		{"too short", "abc123", true},
		{"empty", "", true},
		{"uppercase hex rejected", "ABC123DEF456", true},
		{"non-hex char", "abc123def45g", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateContainerID(c.id)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateContainerID(%q) err=%v, wantErr=%v", c.id, err, c.wantErr)
			}
		})
	}
}

func TestCgroupContainsID(t *testing.T) {
	const id = "abc123def4567890abc123def4567890abc123def4567890abc123def4567890"
	const shortID = "abc123def456"

	cases := []struct {
		name string
		line string
		id   string
		want bool
	}{
		{
			name: "v1 docker path",
			line: "11:memory:/docker/abc123def4567890abc123def4567890abc123def4567890abc123def4567890",
			id:   id,
			want: true,
		},
		{
			name: "v2 systemd scope",
			line: "0::/system.slice/docker-abc123def456.scope",
			id:   shortID,
			want: true,
		},
		{
			name: "v2 plain",
			line: "0::/docker/abc123def456",
			id:   shortID,
			want: true,
		},
		{
			name: "kubepods besteffort",
			line: "10:cpu,cpuacct:/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-podXYZ.slice/cri-containerd-abc123def456.scope",
			id:   shortID,
			want: true,
		},
		{
			name: "substring at tail",
			line: "5:pids:/some/prefix/abc123def456",
			id:   shortID,
			want: true,
		},
		{
			name: "no match",
			line: "11:memory:/system.slice/sshd.service",
			id:   shortID,
			want: false,
		},
		{
			name: "empty line",
			line: "",
			id:   shortID,
			want: false,
		},
		{
			name: "malformed line no colons",
			line: "abc123def456",
			id:   shortID,
			want: false,
		},
		{
			name: "id below threshold rejected even if present",
			line: "11:memory:/docker/abc123",
			id:   "abc123",
			want: false,
		},
		{
			name: "id appears in controller field only (not path)",
			line: "11:abc123def456:/system.slice",
			id:   shortID,
			want: false,
		},

		// ---- Real-world cases observed on Kubernetes + containerd nodes ----
		// These mirror the exact /proc/<pid>/cgroup lines we validated against
		// on clustertdc55 (cri-containerd inside kubepods-besteffort slice).
		{
			name: "cri-containerd v1 full 64-char id",
			line: "11:perf_event:/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod5696ffab_ca6c_4d45_a16f_a164cdbcbf2b.slice/cri-containerd-75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389.scope",
			id:   "75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389",
			want: true,
		},
		{
			name: "cri-containerd matches on 12-char prefix",
			line: "8:memory:/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod5696ffab_ca6c_4d45_a16f_a164cdbcbf2b.slice/cri-containerd-75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389.scope",
			id:   "75c1192030e9",
			want: true,
		},
		{
			name: "cri-containerd v2 unified hierarchy",
			line: "0::/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-podabcdef12.slice/cri-containerd-75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389.scope",
			id:   "75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389",
			want: true,
		},
		{
			name: "containerd ctr default namespace",
			line: "0::/default/abc123def4567890abc123def4567890abc123def4567890abc123def4567890",
			id:   "abc123def4567890abc123def4567890abc123def4567890abc123def4567890",
			want: true,
		},
		{
			name: "cri-o scope path",
			line: "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-podaaaa.slice/crio-abc123def4567890abc123def4567890abc123def4567890abc123def4567890.scope",
			id:   "abc123def456",
			want: true,
		},
		{
			name: "podman user slice",
			line: "0::/user.slice/user-1000.slice/user@1000.service/user.slice/libpod-abc123def4567890abc123def4567890abc123def4567890abc123def4567890.scope",
			id:   "abc123def456",
			want: true,
		},
		{
			name: "mismatched id under different container",
			line: "8:memory:/kubepods.slice/cri-containerd-deadbeefcafebabe0000000000000000000000000000000000000000000000ff.scope",
			id:   "75c1192030e9",
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cgroupContainsID(c.line, c.id)
			if got != c.want {
				t.Fatalf("cgroupContainsID(%q, %q) = %v, want %v", c.line, c.id, got, c.want)
			}
		})
	}
}
