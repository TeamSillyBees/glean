package catalog

import (
	"context"
	"encoding/json"
	"strings"

	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservationmatch"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// LoadPipelineObservation 读取持久化完整处理档案，不读取瘦身列表DTO或改写任何人工审核。
func (s *Service) LoadPipelineObservation(ctx context.Context, candidateID idgen.ID) (ObservationInput, error) {
	if candidateID <= 0 {
		return ObservationInput{}, bizerrors.BadRequest("invalid pipeline candidate ID")
	}
	row, err := s.client.GleanObservation.Query().Where(func(sel *sql.Selector) {
		sel.Where(sqljson.ValueEQ(sel.C(gleanobservation.FieldReview), candidateID.String(), sqljson.Path("pipeline", "candidateId")))
	}).Order(ent.Desc(gleanobservation.FieldUpdatedAt), ent.Desc(gleanobservation.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return ObservationInput{}, bizerrors.BadRequest("论文处理归档不存在")
	}
	if err != nil {
		return ObservationInput{}, bizerrors.WrapDBContext(ctx, err, "load pipeline archive failed")
	}
	return s.decodePipelineObservation(ctx, row)
}

// FindPipelineObservation 先以来源联合索引缩小范围，再查版本key，避免候选淘汰后重复付费处理。
func (s *Service) FindPipelineObservation(ctx context.Context, source, sourceID, key string) (ObservationInput, bool, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(sourceID) == "" || strings.TrimSpace(key) == "" {
		return ObservationInput{}, false, bizerrors.BadRequest("pipeline lookup requires source identity and key")
	}
	row, err := s.client.GleanObservation.Query().Where(gleanobservation.Source(source), gleanobservation.SourceID(sourceID), func(sel *sql.Selector) {
		sel.Where(sqljson.ValueEQ(sel.C(gleanobservation.FieldReview), key, sqljson.Path("pipeline", "key")))
	}).Order(ent.Desc(gleanobservation.FieldUpdatedAt), ent.Desc(gleanobservation.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return ObservationInput{}, false, nil
	}
	if err != nil {
		return ObservationInput{}, false, bizerrors.WrapDBContext(ctx, err, "find pipeline archive failed")
	}
	input, err := s.decodePipelineObservation(ctx, row)
	return input, err == nil, err
}

func (s *Service) decodePipelineObservation(ctx context.Context, row *ent.GleanObservation) (ObservationInput, error) {
	var input ObservationInput
	if err := json.Unmarshal(row.Metadata, &input); err != nil {
		return input, bizerrors.WrapDBContext(ctx, err, "decode pipeline source archive failed")
	}
	encoded, err := json.Marshal(row.Review)
	if err != nil {
		return input, err
	}
	input.Review = ReviewVO{} // 当前review缺失字段不能回退到首次metadata的旧模型结果。
	if err := json.Unmarshal(encoded, &input.Review); err != nil {
		return input, bizerrors.WrapDBContext(ctx, err, "decode pipeline review archive failed")
	}
	input.Pipeline = input.Review.Pipeline
	if len(input.Pipeline) == 0 || string(input.Pipeline) == "null" {
		return input, bizerrors.BadRequest("论文处理归档无效")
	}
	// 主题命中会在幂等重投影时追加；原始metadata保持不可变，恢复时合并真实命中表。
	matches, err := s.client.GleanObservationMatch.Query().Where(gleanobservationmatch.ObservationID(row.ID)).All(ctx)
	if err != nil {
		return input, bizerrors.WrapDBContext(ctx, err, "load pipeline source topics failed")
	}
	topics := append([]string(nil), input.Topics...)
	for _, match := range matches {
		topics = append(topics, match.Topic)
	}
	input.Topics = uniqueSorted(topics)
	return input, nil
}
