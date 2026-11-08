package editorsandbox

import (
	"context"
	"errors"
	"testing"
)

func TestPolicyCannotBeWeakenedIntoTrustedEditorOrWritableDirectory(t *testing.T) {
	required, err := NoPersistence().Requirements()
	if err != nil || !required.DenyFilesystemWrites || !required.DenyNewProcesses || !required.DenyNetwork || !required.DenyNewIPC || !required.RestrictInheritedDescriptors || !required.ApplyBeforePrivateHandoff {
		t.Fatal("incomplete no-persistence boundary")
	}
	required.DenyFilesystemWrites = false
	again, err := NoPersistence().Requirements()
	if err != nil || !again.DenyFilesystemWrites {
		t.Fatal("caller-mutated capability report weakened policy")
	}
	if _, err = (Policy{}).Requirements(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("zero policy was treated as a weaker default")
	}
}

func TestMissingNativeProviderAlwaysFailsClosed(t *testing.T) {
	if Current().Available {
		t.Fatal("unimplemented backend claimed enforcement")
	}
	if err := Require(context.Background(), NoPersistence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing enforcement did not fail closed")
	}
	if err := Require(context.Background(), Policy{}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("invalid policy admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Require(ctx, NoPersistence()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission continued")
	}
}
