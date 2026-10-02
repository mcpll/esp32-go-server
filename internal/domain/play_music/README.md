# play_music

Streams music from a URL (or from bytes, or a pipe) and decodes it into audio frames sent on a channel.

## Functions

```go
PlayMusicStream(ctx, url, sampleRate, frameDuration, audioFormat) (chan []byte, error)
PlayMusicFromAudioData(ctx, audioData, sampleRate, frameDuration, audioFormat) (chan []byte, error)
PlayMusicFromPipe(ctx, pipeReader, sampleRate, frameDuration, audioFormat) (chan []byte, error)
```

- `frameDuration` is in ms; 0 or less means 20.
- `audioFormat` is `"mp3"` by default.
- Only `"mp3"` is supported for streaming. Cancel `ctx` to stop playback.

```go
frames, err := play_music.PlayMusicStream(ctx, "https://example.com/music.mp3", 24000, 20, "mp3")
if err != nil {
    return err
}
for frame := range frames {
    // send the frame to the device
}
```

`types.go` also holds `MusicPlayerConfig`, `StreamingStats` and `PlaybackStatus`.
