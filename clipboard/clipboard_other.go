// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package clipboard

func writePayload(_ Payload) error {
	return ErrUnsupported
}
