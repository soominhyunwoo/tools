package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalker(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	packages, err := Extract(Pkg{
		ImportPath: "github.com/soominhyunwoo/api-generator/parser",
		Dir:        dir,
	})
	require.NoError(t, err)

	require.Len(t, packages, 1)
}
