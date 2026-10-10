package pocketbase

import (
	"context"
	"fmt"
	"strings"

	"xiaozhi-esp32-server-golang/internal/domain/config/store"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"

	"github.com/google/uuid"
)

const speakerSampleError = "error"

// enrollSpeaker sends a sample wav to the voice server and stores the uuid it echoes.
func (p *Provider) enrollSpeaker(ctx context.Context, payload map[string]any) (any, error) {
	sampleID, _ := payload["sample"].(string)
	if strings.TrimSpace(sampleID) == "" {
		return nil, fmt.Errorf("sample is required")
	}
	sample, err := p.client.First(ctx, speakerSamplesCollection, textEquals("id", sampleID), "")
	if err != nil {
		return nil, fmt.Errorf("sample %s: %w", sampleID, err)
	}
	group, err := p.client.First(ctx, speakerGroupsCollection, textEquals("id", sample.String("group")), "")
	if err != nil {
		return nil, fmt.Errorf("speaker group for sample %s: %w", sampleID, err)
	}
	name := strings.TrimSpace(group.String("name"))
	agentID := strings.TrimSpace(group.String("agent"))
	if name == "" || agentID == "" {
		return nil, fmt.Errorf("speaker group %s needs a name and an agent", group.String("id"))
	}
	filename := speakerFileName(sample)
	if filename == "" {
		return nil, fmt.Errorf("sample %s has no wav", sampleID)
	}
	wav, err := p.client.File(ctx, speakerSamplesCollection, sampleID, filename)
	if err != nil {
		return nil, fmt.Errorf("sample %s wav: %w", sampleID, err)
	}
	if len(wav) == 0 {
		return nil, fmt.Errorf("sample %s wav is empty", sampleID)
	}
	owner, err := p.ownerID(ctx)
	if err != nil {
		return nil, err
	}
	baseURL := store.GetString("voice_identify.base_url")
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("voice server is not configured")
	}
	enrolled, err := speaker.Register(ctx, speaker.Enrollment{
		BaseURL:     baseURL,
		UID:         owner,
		AgentID:     agentID,
		SpeakerID:   group.String("id"),
		SpeakerName: name,
		UUID:        uuid.NewString(),
		Filename:    filename,
		Wav:         wav,
	})
	if err != nil {
		_ = p.client.Update(ctx, speakerSamplesCollection, sampleID, map[string]any{"status": speakerSampleError})
		return nil, err
	}
	if err := p.client.Update(ctx, speakerSamplesCollection, sampleID, map[string]any{
		"uuid":   enrolled,
		"status": speakerSampleEnrolled,
	}); err != nil {
		return nil, err
	}
	return map[string]any{"uuid": enrolled}, nil
}

// deleteSpeakerCommand removes one sample embedding, or every embedding in a group.
func (p *Provider) deleteSpeakerCommand(ctx context.Context, payload map[string]any) (any, error) {
	sampleID, _ := payload["sample"].(string)
	groupID, _ := payload["group"].(string)
	sampleID = strings.TrimSpace(sampleID)
	groupID = strings.TrimSpace(groupID)
	switch {
	case sampleID != "" && groupID != "":
		return nil, fmt.Errorf("sample and group are mutually exclusive")
	case sampleID != "":
		return nil, p.deleteSampleEmbedding(ctx, sampleID)
	case groupID != "":
		return nil, p.deleteGroupEmbeddings(ctx, groupID)
	default:
		return nil, fmt.Errorf("sample or group is required")
	}
}

func (p *Provider) deleteSampleEmbedding(ctx context.Context, sampleID string) error {
	sample, err := p.client.First(ctx, speakerSamplesCollection, textEquals("id", sampleID), "")
	if err != nil {
		return fmt.Errorf("sample %s: %w", sampleID, err)
	}
	embedding := strings.TrimSpace(sample.String("uuid"))
	if embedding == "" {
		return fmt.Errorf("sample %s has no embedding", sampleID)
	}
	group, err := p.client.First(ctx, speakerGroupsCollection, textEquals("id", sample.String("group")), "")
	if err != nil {
		return fmt.Errorf("speaker group for sample %s: %w", sampleID, err)
	}
	if err := p.deleteOnVoiceServer(ctx, group.String("agent"), func(baseURL, owner, agentID string) error {
		return speaker.DeleteSample(ctx, baseURL, owner, agentID, embedding)
	}); err != nil {
		return err
	}
	if err := p.client.Delete(ctx, speakerSamplesCollection, sampleID); err != nil {
		return err
	}
	return nil
}

func (p *Provider) deleteGroupEmbeddings(ctx context.Context, groupID string) error {
	group, err := p.client.First(ctx, speakerGroupsCollection, textEquals("id", groupID), "")
	if err != nil {
		return fmt.Errorf("speaker group %s: %w", groupID, err)
	}
	if err := p.deleteOnVoiceServer(ctx, group.String("agent"), func(baseURL, owner, agentID string) error {
		return speaker.DeleteSpeaker(ctx, baseURL, owner, agentID, groupID)
	}); err != nil {
		return err
	}
	samples, err := p.client.List(ctx, speakerSamplesCollection, textEquals("group", groupID), "")
	if err != nil {
		return err
	}
	for _, sample := range samples {
		if err := p.client.Update(ctx, speakerSamplesCollection, sample.String("id"), map[string]any{
			"uuid":   "",
			"status": "pending",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) deleteOnVoiceServer(ctx context.Context, agentID string, fn func(baseURL, owner, agentID string) error) error {
	baseURL := store.GetString("voice_identify.base_url")
	if strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("voice server is not configured")
	}
	owner, err := p.ownerID(ctx)
	if err != nil {
		return err
	}
	return fn(baseURL, owner, strings.TrimSpace(agentID))
}

func (p *Provider) ownerID(ctx context.Context) (string, error) {
	records, err := p.client.List(ctx, usersCollection, "", "")
	if err != nil {
		return "", err
	}
	if len(records) == 0 || records[0].String("id") == "" {
		return "", fmt.Errorf("no console user")
	}
	return records[0].String("id"), nil
}

func speakerFileName(rec Record) string {
	switch value := rec["file"].(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		if len(value) == 0 {
			return ""
		}
		name, _ := value[0].(string)
		return strings.TrimSpace(name)
	default:
		return ""
	}
}
