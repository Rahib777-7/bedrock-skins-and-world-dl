package world

import (
	"bytes"
	"image"
	"image/draw"
	"math"
	"os"
	"time"

	"github.com/bedrock-tool/bedrocktool/utils"
	"golang.design/x/lockfree"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sirupsen/logrus"
	"golang.org/x/image/bmp"
)

const VIEW_MAP_ID = 0x424242

// packet to tell the client that it has a map with id 0x424242 in the offhand
var MAP_ITEM_PACKET packet.InventoryContent = packet.InventoryContent{
	WindowID: 119,
	Content: []protocol.ItemInstance{
		{
			StackNetworkID: 1,
			Stack: protocol.ItemStack{
				ItemType: protocol.ItemType{
					NetworkID:     420,
					MetadataValue: 0,
				},
				BlockRuntimeID: 0,
				Count:          1,
				NBTData: map[string]interface{}{
					"map_uuid": int64(VIEW_MAP_ID),
				},
			},
		},
	},
}

func (m *MapUI) get_bounds() (min, max protocol.ChunkPos) {
	// get the chunk coord bounds
	for _ch := range m.renderedChunks {
		if _ch.X() < min.X() {
			min[0] = _ch.X()
		}
		if _ch.Z() < min.Z() {
			min[1] = _ch.Z()
		}
		if _ch.X() > max.X() {
			max[0] = _ch.X()
		}
		if _ch.Z() > max.Z() {
			max[1] = _ch.Z()
		}
	}
	return
}

type RenderElem struct {
	pos protocol.ChunkPos
	ch  *chunk.Chunk
}

type MapUI struct {
	img            *image.RGBA // rendered image
	zoomLevel      int         // pixels per chunk
	renderQueue    *lockfree.Queue
	renderedChunks map[protocol.ChunkPos]*image.RGBA // prerendered chunks
	needRedraw     bool                              // when the map has updated this is true

	ticker *time.Ticker
	w      *WorldState
}

func NewMapUI(w *WorldState) *MapUI {
	m := &MapUI{
		img:            image.NewRGBA(image.Rect(0, 0, 128, 128)),
		zoomLevel:      16,
		renderQueue:    lockfree.NewQueue(),
		renderedChunks: make(map[protocol.ChunkPos]*image.RGBA),
		needRedraw:     true,
		w:              w,
	}
	return m
}

func (m *MapUI) Start() {
	m.ticker = time.NewTicker(33 * time.Millisecond)
	go func() {
		for range m.ticker.C {
			if m.needRedraw {
				m.needRedraw = false
				m.Redraw()

				if m.w.proxy.Client != nil {
					if err := m.w.proxy.Client.WritePacket(&packet.ClientBoundMapItemData{
						MapID:       VIEW_MAP_ID,
						Width:       128,
						Height:      128,
						Pixels:      utils.Img2rgba(m.img),
						UpdateFlags: 2,
					}); err != nil {
						logrus.Error(err)
						return
					}
				}
			}
		}
	}()
}

func (m *MapUI) Stop() {
	if m.ticker != nil {
		m.ticker.Stop()
	}
}

// Reset resets the map to inital state
func (m *MapUI) Reset() {
	m.renderedChunks = make(map[protocol.ChunkPos]*image.RGBA)
	m.SchedRedraw()
}

// ChangeZoom adds to the zoom value and goes around to 32 once it hits 128
func (m *MapUI) ChangeZoom() {
	m.zoomLevel /= 2
	if m.zoomLevel == 0 {
		m.zoomLevel = 16
	}
	m.SchedRedraw()
}

// SchedRedraw tells the map to redraw the next time its sent
func (m *MapUI) SchedRedraw() {
	m.needRedraw = true
}

// draw_img_scaled_pos draws src onto dst at bottom_left, scaled to size
func draw_img_scaled_pos(dst *image.RGBA, src *image.RGBA, bottom_left image.Point, size_scaled int) {
	sbx := src.Bounds().Dx()
	ratio := int(float64(sbx) / float64(size_scaled))

	for x_out := bottom_left.X; x_out < bottom_left.X+size_scaled; x_out++ {
		for y_out := bottom_left.Y; y_out < bottom_left.Y+size_scaled; y_out++ {
			x_in := (x_out - bottom_left.X) * ratio
			y_in := (y_out - bottom_left.Y) * ratio
			c := src.At(x_in, y_in)
			dst.Set(x_out, y_out, c)
		}
	}
}

// draw chunk images to the map image
func (m *MapUI) Redraw() {
	for {
		r, ok := m.renderQueue.Dequeue().(*RenderElem)
		if !ok {
			break
		}
		if r.ch != nil {
			m.renderedChunks[r.pos] = Chunk2Img(r.ch)
		} else {
			m.renderedChunks[r.pos] = black_16x16
		}
	}

	middle := protocol.ChunkPos{
		int32(m.w.PlayerPos.Position.X()),
		int32(m.w.PlayerPos.Position.Z()),
	}

	// total_width := 32 * math.Ceil(float64(chunks_x)/32)
	chunks_per_line := float64(128 / m.zoomLevel)
	px_per_block := 128 / chunks_per_line / 16 // how many pixels per block
	sz_chunk := int(math.Floor(px_per_block * 16))

	for i := 0; i < len(m.img.Pix); i++ { // clear canvas
		m.img.Pix[i] = 0
	}

	for _ch := range m.renderedChunks {
		relative_middle_x := float64(_ch.X()*16 - middle.X())
		relative_middle_z := float64(_ch.Z()*16 - middle.Z())
		px_pos := image.Point{ // bottom left corner of the chunk on the map
			X: int(math.Floor(relative_middle_x*px_per_block)) + 64,
			Y: int(math.Floor(relative_middle_z*px_per_block)) + 64,
		}

		if !m.img.Rect.Intersect(image.Rect(px_pos.X, px_pos.Y, px_pos.X+sz_chunk, px_pos.Y+sz_chunk)).Empty() {
			draw_img_scaled_pos(m.img, m.renderedChunks[_ch], px_pos, sz_chunk)
		}
	}

	draw_full := false

	if draw_full {
		img2 := m.ToImage()
		buf := bytes.NewBuffer(nil)
		bmp.Encode(buf, img2)
		os.WriteFile("test.bmp", buf.Bytes(), 0o777)
	}
}

func (m *MapUI) ToImage() *image.RGBA {
	// get the chunk coord bounds
	min, max := m.get_bounds()
	chunks_x := int(max[0] - min[0] + 1) // how many chunk lengths is x coordinate
	chunks_y := int(max[1] - min[1] + 1)

	img2 := image.NewRGBA(image.Rect(0, 0, chunks_x*16, chunks_y*16))

	middle_block_x := chunks_x / 2 * 16
	middle_block_y := chunks_y / 2 * 16

	for pos := range m.renderedChunks {
		px_pos := image.Point{
			X: int(pos.X()*16) - middle_block_x + img2.Rect.Dx(),
			Y: int(pos.Z()*16) - middle_block_y + img2.Rect.Dy(),
		}
		draw.Draw(img2, image.Rect(
			px_pos.X,
			px_pos.Y,
			px_pos.X+16,
			px_pos.Y+16,
		), m.renderedChunks[pos], image.Point{}, draw.Src)
	}
	return img2
}

func (m *MapUI) SetChunk(pos protocol.ChunkPos, ch *chunk.Chunk) {
	m.renderQueue.Enqueue(&RenderElem{pos, ch})
	m.SchedRedraw()
}
