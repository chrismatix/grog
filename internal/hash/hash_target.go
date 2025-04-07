package main

import (
	"fmt"
	"github.com/cespare/xxhash/v2"
	"grog/internal/model"
	"slices"
	"strings"
)

// GetTargetChangeHash computes the hash that tells us if a target has changed.
func GetTargetChangeHash(target model.Target) (string, error) {

	return hashTargetDefinition(target)
}

// hashTargetDefinition computes the xxhash hash of a single file.
func hashTargetDefinition(target model.Target) (string, error) {
	hasher := xxhash.New()

	_, err := hasher.WriteString(target.Label.String())
	_, err = hasher.WriteString(target.Command)
	_, err = hasher.WriteString(sorted(target.Inputs))
	_, err = hasher.WriteString(sorted(target.Outputs))
	_, err = hasher.WriteString(sorted(target.GetDepsString()))

	if err != nil {
		return "", err
	}
	// Return the hash as a hexadecimal string.
	return fmt.Sprintf("%x", hasher.Sum64()), nil
}

func sorted(s []string) string {
	slices.Sort(s)
	return strings.Join(s, ",")
}
