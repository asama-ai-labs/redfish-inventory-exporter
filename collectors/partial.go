// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"errors"
	"log"

	"github.com/stmcginnis/gofish/common"
)

// isPartial reports whether err is gofish's *common.CollectionError. gofish returns
// every member it could read together with that error, so the list is still usable:
// one DIMM that answers 404 must not remove every other DIMM from the scrape.
func isPartial(err error) bool {
	var ce *common.CollectionError
	return errors.As(err, &ce)
}

// keepPartial decides what to do with the error from listing a collection. It returns
// ok=false for an error that leaves nothing usable. For a partial result it logs the
// failed members and records the error in *partial, and returns ok=true.
func keepPartial(what string, err error, partial *[]error) (ok bool) {
	if err == nil {
		return true
	}
	if !isPartial(err) {
		return false
	}
	log.Printf("%s: some members could not be read, keeping the rest: %v", what, err)
	*partial = append(*partial, err)
	return true
}
