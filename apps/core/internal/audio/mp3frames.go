package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The listener's player decodes each piece on its own with decodeAudioData,
// so a piece must hold whole MP3 frames, and every seam costs a few
// milliseconds of decoder priming. The first piece is small so playback
// starts at once; later ones grow so seams become rare.
const (
	firstPieceBytes = 48 << 10
	maxPieceBytes   = 256 << 10
)

// framesFromMP3 relays an MP3 byte stream as length-prefixed pieces
// (tts.FramesContentType), each cut on an MP3 frame boundary. A stream that
// ends inside a frame is an error: the reading was cut short.
func framesFromMP3(src io.Reader, dst io.Writer) error {
	var pending []byte
	scanned := 0
	target := firstPieceBytes
	buf := make([]byte, 32<<10)

	flush := func() error {
		if scanned == 0 {
			return nil
		}
		var head [4]byte
		binary.BigEndian.PutUint32(head[:], uint32(scanned))
		if _, err := dst.Write(head[:]); err != nil {
			return err
		}
		if _, err := dst.Write(pending[:scanned]); err != nil {
			return err
		}
		pending = append(pending[:0], pending[scanned:]...)
		scanned = 0
		target = min(target*2, maxPieceBytes)
		return nil
	}

	for {
		n, readErr := src.Read(buf)
		pending = append(pending, buf[:n]...)

		for {
			size, ok, err := nextMP3Unit(pending[scanned:])
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			scanned += size
		}
		if scanned >= target {
			if err := flush(); err != nil {
				return err
			}
		}

		if errors.Is(readErr, io.EOF) {
			if scanned != len(pending) {
				return fmt.Errorf("mp3 stream ends inside a frame (%d bytes left)", len(pending)-scanned)
			}
			return flush()
		}
		if readErr != nil {
			return readErr
		}
	}
}

// nextMP3Unit reports the size of the frame or tag at the head of b, and
// false while b does not hold all of it yet.
func nextMP3Unit(b []byte) (int, bool, error) {
	if len(b) < 4 {
		return 0, false, nil
	}
	switch {
	case string(b[:3]) == "ID3":
		if len(b) < 10 {
			return 0, false, nil
		}
		size := 10 + (int(b[6]&0x7f)<<21 | int(b[7]&0x7f)<<14 | int(b[8]&0x7f)<<7 | int(b[9]&0x7f))
		if b[5]&0x10 != 0 {
			size += 10
		}
		return size, len(b) >= size, nil
	case string(b[:3]) == "TAG":
		return 128, len(b) >= 128, nil
	}
	size, err := mp3FrameSize(b)
	if err != nil {
		return 0, false, err
	}
	return size, len(b) >= size, nil
}

var (
	layer3Kbps = [2][15]int{
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
	sampleRates = map[byte][3]int{
		3: {44100, 48000, 32000},
		2: {22050, 24000, 16000},
		0: {11025, 12000, 8000},
	}
)

func mp3FrameSize(h []byte) (int, error) {
	if h[0] != 0xff || h[1]&0xe0 != 0xe0 {
		return 0, fmt.Errorf("mp3: no frame sync at % x", h[:4])
	}
	version := (h[1] >> 3) & 3
	layer := (h[1] >> 1) & 3
	bitrateIdx := h[2] >> 4
	rateIdx := (h[2] >> 2) & 3
	rates, ok := sampleRates[version]
	if !ok || layer != 1 || bitrateIdx == 0 || bitrateIdx == 15 || rateIdx == 3 {
		return 0, fmt.Errorf("mp3: unsupported frame header % x", h[:4])
	}
	padding := int(h[2]>>1) & 1
	if version == 3 {
		return 144000*layer3Kbps[0][bitrateIdx]/rates[rateIdx] + padding, nil
	}
	return 72000*layer3Kbps[1][bitrateIdx]/rates[rateIdx] + padding, nil
}
