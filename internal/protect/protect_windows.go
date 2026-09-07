//go:build windows

package protect

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ownerOnly replaces the ACL with one entry: the current user, full control,
// and a protected DACL so the parent directory cannot put everyone else back
// on the list. That is the Windows answer to Unix 0600 / 0700 — Go's own chmod
// only flips the read-only attribute there.
func ownerOnly(path string, directory bool) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	// TrusteeValueFromSID stores a raw pointer. The SID must stay pinned for
	// the lifetime of the TrusteeValue — otherwise the GC can move it under
	// SetEntriesInAcl and the ACL names a garbage address.
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()

	var inheritance uint32 = windows.NO_INHERITANCE
	if directory {
		// Children inherit the same owner-only ACE. Files we write still call
		// OwnerOnly themselves; inheritance covers anything else that lands
		// under .yagit/ (a log, a screenshot) without a second code path.
		inheritance = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}

	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("building an owner-only ACL: %w", err)
	}

	// PROTECTED_DACL stops inherited ACEs from the parent merging back in.
	// Without it, an explicit "only me" entry would sit beside whatever the
	// parent still contributes.
	info := windows.SECURITY_INFORMATION(
		windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		info,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("setting an owner-only ACL: %w", err)
	}
	return nil
}

// checkOwnerOnly confirms the DACL is protected and names only the current
// user. Mode bits are not what governs access on this platform.
func checkOwnerOnly(path string, _ bool) error {
	want, err := currentUserSID()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("DACL is not protected")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return fmt.Errorf("DACL is nil")
	}
	if dacl.AceCount != 1 {
		return fmt.Errorf("DACL has %d ACEs, want 1", dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		return err
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return fmt.Errorf("ACE type is %d, want ACCESS_ALLOWED", ace.Header.AceType)
	}
	got := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !windows.EqualSid(got, want) {
		return fmt.Errorf("trustee is %s, want %s", got, want)
	}
	return nil
}

// currentUserSID is the SID of the account this process runs as.
func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("reading the process token: %w", err)
	}
	return user.User.Sid, nil
}
