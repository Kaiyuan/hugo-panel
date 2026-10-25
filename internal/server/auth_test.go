package server

import (
	"strings"
	"testing"
)

func TestPasswordHashingHandlesLongPasswords(t *testing.T) {
	password := strings.Repeat("长密码-", 40)
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == password || len(hash) < 50 {
		t.Fatalf("unexpected hash: %q", hash)
	}
	if !verifyPassword(hash, password) {
		t.Fatal("correct password did not verify")
	}
	if verifyPassword(hash, password+"x") {
		t.Fatal("wrong password verified")
	}
}

func TestValidatePassword(t *testing.T) {
	if validatePassword("short") == nil {
		t.Fatal("short password should fail")
	}
	if err := validatePassword("long-enough"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}
