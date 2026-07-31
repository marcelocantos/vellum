// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package clipboard

func writePayload(_ Payload) error {
	return ErrUnsupported
}

func readClipboard(_ string) ([]byte, error) {
	return nil, ErrUnsupported
}

func writeFileRefs(_ []string) error {
	return ErrUnsupported
}

func readFileRefs() ([]string, error) {
	return nil, ErrUnsupported
}
