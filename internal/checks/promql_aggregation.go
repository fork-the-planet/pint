package checks

import (
	"context"
	"fmt"

	"github.com/cloudflare/pint/internal/diags"
	"github.com/cloudflare/pint/internal/discovery"
	"github.com/cloudflare/pint/internal/parser/utils"
)

const (
	AggregationCheckName = "promql/aggregate"
)

func NewAggregationCheck(nameRegex *TemplatedRegexp, label string, keep bool, comment string, severity Severity) AggregationCheck {
	return AggregationCheck{
		nameRegex: nameRegex,
		label:     label,
		keep:      keep,
		comment:   comment,
		severity:  severity,
	}
}

type AggregationCheck struct {
	nameRegex *TemplatedRegexp
	label     string
	comment   string
	severity  Severity
	keep      bool
}

func (c AggregationCheck) Meta() CheckMeta {
	return CheckMeta{
		States: []discovery.ChangeType{
			discovery.Noop,
			discovery.Added,
			discovery.Modified,
			discovery.Moved,
		},
		Online:        false,
		AlwaysEnabled: false,
	}
}

func (c AggregationCheck) String() string {
	return fmt.Sprintf("%s(%s:%v)", AggregationCheckName, c.label, c.keep)
}

func (c AggregationCheck) Reporter() string {
	return AggregationCheckName
}

func (c AggregationCheck) Check(_ context.Context, entry discovery.Entry, _ []discovery.Entry) (problems []Problem) {
	expr := entry.Rule.Expr()
	if expr.SyntaxError != nil {
		return nil
	}

	if c.nameRegex != nil {
		if entry.Rule.RecordingRule != nil && !c.nameRegex.MustExpand(entry.Rule).MatchString(entry.Rule.RecordingRule.Record.Value) {
			return nil
		}
		if entry.Rule.AlertingRule != nil && !c.nameRegex.MustExpand(entry.Rule).MatchString(entry.Rule.AlertingRule.Alert.Value) {
			return nil
		}
	}

	if entry.Rule.RecordingRule != nil && entry.Rule.RecordingRule.Labels != nil {
		if val := entry.Rule.RecordingRule.Labels.GetValue(c.label); val != nil {
			return nil
		}
	}

	if entry.Rule.AlertingRule != nil && entry.Rule.AlertingRule.Labels != nil {
		if val := entry.Rule.AlertingRule.Labels.GetValue(c.label); val != nil {
			return nil
		}
	}

	nameDesc := "`" + c.nameRegex.anchored + "`"
	if nameDesc == "`^.+$`" || nameDesc == "`^.*$`" {
		nameDesc = "all"
	}

	for _, src := range utils.LabelsSource(expr.Value.Value, expr.Query.Expr) {
		if src.Type != utils.AggregateSource {
			continue
		}
		if c.keep && !src.CanHaveLabel(c.label) {
			el := src.LabelExcludeReason(c.label)
			problems = append(problems, Problem{
				Anchor:   AnchorAfter,
				Lines:    expr.Value.Pos.Lines(),
				Reporter: c.Reporter(),
				Summary:  "required label is being removed via aggregation",
				Details:  maybeComment(c.comment),
				Diagnostics: []diags.Diagnostic{
					{
						Message:     el.Reason,
						Pos:         expr.Value.Pos,
						FirstColumn: int(el.Fragment.Start) + 1,
						LastColumn:  int(el.Fragment.End),
						Kind:        diags.Context,
					},
					{
						Message: fmt.Sprintf("`%s` label is required and should be preserved when aggregating %s rules.",
							c.label, nameDesc),
						Pos:         expr.Value.Pos,
						FirstColumn: int(el.Fragment.Start) + 1,
						LastColumn:  int(el.Fragment.End),
						Kind:        diags.Issue,
					},
				},
				Severity: c.severity,
			})
		}
		if !c.keep && src.CanHaveLabel(c.label) {
			posrange := src.Position
			if src.Aggregation != nil {
				posrange = src.Aggregation.PosRange
				if len(src.Aggregation.Grouping) != 0 {
					if src.Aggregation.Without {
						posrange = utils.FindPosition(expr.Value.Value, src.Aggregation.PosRange, "without")
					} else {
						posrange = utils.FindPosition(expr.Value.Value, src.Aggregation.PosRange, "by")
					}
				}
			}
			problems = append(problems, Problem{
				Anchor:   AnchorAfter,
				Lines:    expr.Value.Pos.Lines(),
				Reporter: c.Reporter(),
				Summary:  "label must be removed in aggregations",
				Details:  maybeComment(c.comment),
				Diagnostics: []diags.Diagnostic{
					{
						Message: fmt.Sprintf("`%s` label should be removed when aggregating %s rules.",
							c.label, nameDesc),
						Pos:         expr.Value.Pos,
						FirstColumn: int(posrange.Start) + 1,
						LastColumn:  int(posrange.End),
						Kind:        diags.Issue,
					},
				},
				Severity: c.severity,
			})
		}
	}

	return problems
}
