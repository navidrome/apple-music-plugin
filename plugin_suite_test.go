package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAppleMusicPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Apple Music Plugin Suite")
}
