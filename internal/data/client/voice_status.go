package client

type VoiceStatus struct {
	HaveVoice            bool  //whether speech was present last time
	HaveVoiceLastTime    int64 //last speech time
	VoiceStop            bool  //whether speech stopped
	SilenceThresholdTime int64 //silence duration threshold
}

func (v *VoiceStatus) Reset() {
	v.HaveVoice = false
	v.HaveVoiceLastTime = 0
	v.VoiceStop = false
}

func (v *VoiceStatus) IsSilence(diffMilli int64) bool {
	return diffMilli > v.SilenceThresholdTime
}

func (v *VoiceStatus) GetClientHaveVoice() bool {
	return v.HaveVoice
}

func (v *VoiceStatus) SetClientHaveVoice(haveVoice bool) {
	v.HaveVoice = haveVoice
}

func (v *VoiceStatus) GetClientHaveVoiceLastTime() int64 {
	return v.HaveVoiceLastTime
}

func (v *VoiceStatus) SetClientHaveVoiceLastTime(lastTime int64) {
	v.HaveVoiceLastTime = lastTime
}

func (v *VoiceStatus) GetClientVoiceStop() bool {
	return v.VoiceStop
}

func (v *VoiceStatus) SetClientVoiceStop(voiceStop bool) {
	v.VoiceStop = voiceStop
}
