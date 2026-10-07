package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func mp3Frame(padded bool) []byte {
	head := []byte{0xff, 0xfb, 0x90, 0x00}
	size := 417
	if padded {
		head[2] |= 0x02
		size = 418
	}
	return append(head, bytes.Repeat([]byte{0x55}, size-4)...)
}

func id3Tag(payload int) []byte {
	tag := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, byte(payload >> 7 & 0x7f), byte(payload & 0x7f)}
	return append(tag, bytes.Repeat([]byte{0}, payload)...)
}

func readPieces(t *testing.T, framed []byte) [][]byte {
	t.Helper()
	var pieces [][]byte
	for len(framed) > 0 {
		n := binary.BigEndian.Uint32(framed)
		pieces = append(pieces, framed[4:4+n])
		framed = framed[4+n:]
	}
	return pieces
}

func TestFramesFromMP3CutsOnFrameBoundaries(t *testing.T) {
	var stream []byte
	stream = append(stream, id3Tag(300)...)
	var frameStarts = map[int]bool{len(stream): true}
	for i := range 2000 {
		stream = append(stream, mp3Frame(i%3 == 0)...)
		frameStarts[len(stream)] = true
	}

	var out bytes.Buffer
	if err := framesFromMP3(iotest.OneByteReader(bytes.NewReader(stream)), &out); err != nil {
		t.Fatal(err)
	}
	pieces := readPieces(t, out.Bytes())
	if len(pieces) < 3 {
		t.Fatalf("got %d pieces, want the stream relayed in several", len(pieces))
	}
	if len(pieces[0]) > firstPieceBytes+418+310 {
		t.Errorf("first piece is %d bytes, it should be about %d", len(pieces[0]), firstPieceBytes)
	}

	offset := 0
	for i, p := range pieces {
		offset += len(p)
		if !frameStarts[offset] {
			t.Fatalf("piece %d ends mid-frame at byte %d", i, offset)
		}
	}
	if !bytes.Equal(bytes.Join(pieces, nil), stream) {
		t.Fatal("pieces do not join back into the stream")
	}
}

func TestFramesFromMP3RejectsATruncatedStream(t *testing.T) {
	stream := append(mp3Frame(false), mp3Frame(false)[:100]...)
	err := framesFromMP3(bytes.NewReader(stream), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "inside a frame") {
		t.Fatalf("err = %v", err)
	}
}

func TestFramesFromMP3RejectsWhatIsNotMP3(t *testing.T) {
	err := framesFromMP3(strings.NewReader(`{"detail":"not audio"}`), io.Discard)
	if err == nil {
		t.Fatal("JSON passed for MP3")
	}
}
