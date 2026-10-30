//go:build !darwin

package agy

func keychainHasToken() bool { return false }
