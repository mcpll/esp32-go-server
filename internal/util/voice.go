package util

import (
	"bytes"
	"encoding/binary"
	"math"
)

// PCM16BytesToFloat32 converts 16-bit little-endian PCM bytes to float32 (-1.0~1.0)
func PCM16BytesToFloat32(pcm []byte) []float32 {
	n := len(pcm) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		// Take two bytes as little-endian int16
		sample := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
		out[i] = float32(sample) / float32(math.MaxInt16)
	}
	return out
}

// float32ToPCMBytes converts float32 to 16-bit PCM bytes
func Float32ToPCMBytes(samples []float32, pcmBytes []byte) {
	for i, sample := range samples {
		// Convert float32 (-1.0 to 1.0) to int16 (-32768 to 32767)
		intSample := float32ToInt16(sample)

		// Write little-endian into byte slice
		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(intSample))
	}

	return
}

// Float32ToInt16 converts float32 (-1.0~1.0) to int16 (-32768~32767)
func float32ToInt16(sample float32) int16 {
	if sample > 1.0 {
		return 32767
	} else if sample < -1.0 {
		return -32768
	} else {
		return int16(sample * 32767)
	}
}

// Float32SliceToInt16Slice converts float32 slice to int16 slice
func Float32SliceToInt16Slice(samples []float32) []int16 {
	result := make([]int16, len(samples))
	for i, sample := range samples {
		result[i] = float32ToInt16(sample)
	}
	return result
}

// int16SliceToBytes converts int16 slice to []byte (little-endian)
func Int16SliceToBytes(samples []int16) []byte {
	buf := new(bytes.Buffer)
	for _, s := range samples {
		buf.WriteByte(byte(s))
		buf.WriteByte(byte(s >> 8))
	}
	return buf.Bytes()
}

func ResampleLinearFloat32(input []float32, inRate, outRate int) []float32 {
	ratio := float64(outRate) / float64(inRate)
	outLen := int(float64(len(input)) * ratio)
	output := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		pos := float64(i) / ratio
		index := int(pos)
		if index >= len(input)-1 {
			output[i] = input[len(input)-1]
		} else {
			frac := float32(pos - float64(index))
			output[i] = input[index]*(1-frac) + input[index+1]*frac
		}
	}
	return output
}

// Float32SliceToBytes converts float32 to little-endian bytes (4 bytes each)
func Float32SliceToBytes(data []float32) []byte {
	if len(data) == 0 {
		return nil
	}
	bytes := make([]byte, len(data)*4)
	for i, v := range data {
		binary.LittleEndian.PutUint32(bytes[i*4:i*4+4], math.Float32bits(v))
	}
	return bytes
}
