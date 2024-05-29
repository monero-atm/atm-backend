package main

import (
	"fmt"
	"strings"
)

func parseAddress(addr string) string {
	_, addressWithParams, found0 := strings.Cut(addr, ":")
	if !found0 {
		return addr
	}
	address, _, found1 := strings.Cut(addressWithParams, "?")
	if !found1 {
		return addressWithParams
	}
	return address
}

func addressValidator(s string) error {
	if len(s) != 95 {
		return fmt.Errorf("Invalid address length")
	}
	if cfg.Mode == "mainnet" && !(s[0] == '8' || s[0] == '4') {
		return fmt.Errorf("Invalid mainnet address")
	}
	if cfg.Mode == "stagenet" && !(s[0] == '7' || s[0] == '5') {
		return fmt.Errorf("Invalid stagenet address")
	}
	return nil
}
