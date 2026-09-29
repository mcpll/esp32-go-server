package client

import "time"

// Statistic is deprecated; use statistic_plugin at MetricTtsStop for stats
type Statistic struct {
	TurnStartTs        int64
	VoiceSilenceTs     int64
	AsrFirstTextTs     int64
	AsrFinalTextTs     int64
	LlmStartTs         int64
	LlmFirstTokenTs    int64
	LlmFirstSentenceTs int64
	LlmEndTs           int64
	TtsStartTs         int64
	TtsFirstFrameTs    int64
	TtsStopTs          int64
}

// MarkTurnStart records turn start time
func (state *ClientState) MarkTurnStart() {
	state.Statistic.TurnStartTs = time.Now().UnixMilli()
	state.Statistic.VoiceSilenceTs = 0
	state.Statistic.AsrFirstTextTs = 0
	state.Statistic.AsrFinalTextTs = 0
}

// MarkVoiceSilenceAt records voice silence start; returns whether first in this turn
func (state *ClientState) MarkVoiceSilenceAt(ts int64) bool {
	if state.Statistic.VoiceSilenceTs != 0 {
		return false
	}
	state.Statistic.VoiceSilenceTs = ts
	return true
}

// MarkVoiceSilence records voice silence start; returns whether first in this turn
func (state *ClientState) MarkVoiceSilence() bool {
	return state.MarkVoiceSilenceAt(time.Now().UnixMilli())
}

// MarkAsrFirstText records first ASR text time
func (state *ClientState) MarkAsrFirstText() {
	if state.Statistic.AsrFirstTextTs == 0 {
		state.Statistic.AsrFirstTextTs = time.Now().UnixMilli()
	}
}

// MarkAsrFinalText records final ASR text time
func (state *ClientState) MarkAsrFinalText() {
	state.MarkAsrFinalTextAt(time.Now().UnixMilli())
}

// MarkAsrFinalTextAt records final ASR text time; returns whether first in this turn
func (state *ClientState) MarkAsrFinalTextAt(ts int64) bool {
	if state.Statistic.AsrFinalTextTs != 0 {
		return false
	}
	state.Statistic.AsrFinalTextTs = ts
	return true
}

// MarkLlmStart records LLM start time
func (state *ClientState) MarkLlmStart() {
	state.Statistic.LlmStartTs = time.Now().UnixMilli()
	state.Statistic.LlmFirstTokenTs = 0
	state.Statistic.LlmFirstSentenceTs = 0
	state.Statistic.LlmEndTs = 0
}

// MarkLlmFirstToken records first LLM token time
func (state *ClientState) MarkLlmFirstToken() {
	state.Statistic.LlmFirstTokenTs = time.Now().UnixMilli()
}

// MarkLlmFirstSentenceAt records first LLM sentence time; returns whether first in this turn
func (state *ClientState) MarkLlmFirstSentenceAt(ts int64) bool {
	if state.Statistic.LlmFirstSentenceTs != 0 {
		return false
	}
	state.Statistic.LlmFirstSentenceTs = ts
	return true
}

// MarkLlmFirstSentence records first LLM sentence time; returns whether first in this turn
func (state *ClientState) MarkLlmFirstSentence() bool {
	return state.MarkLlmFirstSentenceAt(time.Now().UnixMilli())
}

// MarkLlmEnd records LLM end time
func (state *ClientState) MarkLlmEnd() {
	state.Statistic.LlmEndTs = time.Now().UnixMilli()
}

// MarkTtsStart records TTS start time
func (state *ClientState) MarkTtsStart() {
	state.Statistic.TtsStartTs = time.Now().UnixMilli()
	state.Statistic.TtsFirstFrameTs = 0
	state.Statistic.TtsStopTs = 0
}

// MarkTtsFirstFrame records TTS first-frame time
func (state *ClientState) MarkTtsFirstFrame() {
	if state.Statistic.TtsFirstFrameTs == 0 {
		state.Statistic.TtsFirstFrameTs = time.Now().UnixMilli()
	}
}

// MarkTtsStop records TTS end time
func (state *ClientState) MarkTtsStop() {
	state.Statistic.TtsStopTs = time.Now().UnixMilli()
}

// SetStartAsrTs sets ASR start time (alias for compatibility)
func (state *ClientState) SetStartAsrTs() { state.MarkVoiceSilence() }

// SetStartLlmTs sets LLM start time (alias for compatibility)
func (state *ClientState) SetStartLlmTs() { state.MarkLlmStart() }

// SetStartTtsTs sets TTS start time (alias for compatibility)
func (state *ClientState) SetStartTtsTs() { state.MarkTtsStart() }

// GetAsrDuration returns ASR duration (deprecated; signature only)
func (state *ClientState) GetAsrDuration() int64 {
	return calcStatisticDuration(state.Statistic.VoiceSilenceTs, state.Statistic.AsrFinalTextTs)
}

// GetAsrLlmTtsDuration returns overall duration (deprecated; signature only)
func (state *ClientState) GetAsrLlmTtsDuration() int64 {
	return calcStatisticDuration(state.Statistic.VoiceSilenceTs, state.Statistic.TtsFirstFrameTs)
}

// GetLlmDuration returns LLM duration (deprecated; signature only)
func (state *ClientState) GetLlmDuration() int64 {
	return calcStatisticDuration(state.Statistic.LlmStartTs, state.Statistic.LlmEndTs)
}

// GetTtsDuration returns TTS duration (deprecated; signature only)
func (state *ClientState) GetTtsDuration() int64 {
	return calcStatisticDuration(state.Statistic.TtsStartTs, state.Statistic.TtsStopTs)
}

func calcStatisticDuration(start, end int64) int64 {
	if start <= 0 || end <= 0 || end < start {
		return 0
	}
	return end - start
}

func (s *Statistic) Reset() {
	if s == nil {
		return
	}
	*s = Statistic{}
}
