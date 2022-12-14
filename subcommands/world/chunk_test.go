package world

import (
	"image/png"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func Test(t *testing.T) {
	data, _ := os.ReadFile("chunk.bin")
	ch, _, _ := chunk.NetworkDecode(33, data, 6, cube.Range{0, 255}, true)
	i := Chunk2Img(ch)
	f, _ := os.Create("chunk.png")
	png.Encode(f, i)
	f.Close()
}

func Benchmark_chunk_decode(b *testing.B) {
	data, _ := os.ReadFile("chunk.bin")
	for i := 0; i < b.N; i++ {
		_, _, err := chunk.NetworkDecode(33, data, 6, cube.Range{0, 255}, true)
		if err != nil {
			b.Error(err)
		}
	}
}

func Benchmark_render_chunk(b *testing.B) {
	data, _ := os.ReadFile("chunk.bin")
	ch, _, _ := chunk.NetworkDecode(33, data, 6, cube.Range{0, 255}, true)

	for i := 0; i < b.N; i++ {
		Chunk2Img(ch)
	}
}
