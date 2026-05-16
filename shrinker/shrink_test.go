package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroup(t *testing.T) {
	require.Equal(t, "sum_pillow", getGroup("/home/ern/tes3/mods/TexturePacks/Demake/04 Tamriel Data/textures/sum/m/sum_pillow_05.dds"))

}
