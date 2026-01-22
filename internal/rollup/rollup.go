package rollup

import (
	"fmt"
	"sync"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type CardinalityLevel int

const (
	LevelFine CardinalityLevel = iota
	LevelMedium
	LevelCoarse
)

type Processor struct {
	cfg config.RollupConfig
	mu  sync.RWMutex
	
	currentLevel CardinalityLevel
}

func New(cfg config.RollupConfig) *Processor {
	return &Processor{
		cfg:          cfg,
		currentLevel: LevelFine,
	}
}

func (p *Processor) SetLevel(level CardinalityLevel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentLevel = level
}

func (p *Processor) RollupMetrics(signals []*signals.MetricSignal) pmetric.Metrics {
	p.mu.RLock()
	level := p.currentLevel
	p.mu.RUnlock()

	if level == LevelFine || !p.cfg.Enabled {
		return p.mergeMetrics(signals)
	}

	out := pmetric.NewMetrics()
	
	// Trackers for merging
	// Key: string representation of Resource attributes
	resMap := make(map[string]pmetric.ResourceMetrics)

	for _, sig := range signals {
		rms := sig.Metrics.ResourceMetrics()
		for i := 0; i < rms.Len(); i++ {
			rm := rms.At(i)
			resKey := p.attrKey(rm.Resource().Attributes())
			
			destRM, ok := resMap[resKey]
			if !ok {
				destRM = out.ResourceMetrics().AppendEmpty()
				rm.Resource().CopyTo(destRM.Resource())
				resMap[resKey] = destRM
			}
			
			p.mergeScopeMetrics(destRM, rm, level)
		}
	}

	return out
}

func (p *Processor) mergeScopeMetrics(destRM pmetric.ResourceMetrics, srcRM pmetric.ResourceMetrics, level CardinalityLevel) {
	scopeMap := make(map[string]pmetric.ScopeMetrics)
	
	// Init scopeMap with existing ones in destRM
	for i := 0; i < destRM.ScopeMetrics().Len(); i++ {
		sm := destRM.ScopeMetrics().At(i)
		scopeMap[p.scopeKey(sm.Scope())] = sm
	}

	sms := srcRM.ScopeMetrics()
	for i := 0; i < sms.Len(); i++ {
		srcSM := sms.At(i)
		sk := p.scopeKey(srcSM.Scope())
		
		destSM, ok := scopeMap[sk]
		if !ok {
			destSM = destRM.ScopeMetrics().AppendEmpty()
			srcSM.Scope().CopyTo(destSM.Scope())
			scopeMap[sk] = destSM
		}
		
		p.mergeMetricsList(destSM, srcSM, level)
	}
}

func (p *Processor) mergeMetricsList(destSM pmetric.ScopeMetrics, srcSM pmetric.ScopeMetrics, level CardinalityLevel) {
	metricMap := make(map[string]pmetric.Metric)
	
	for i := 0; i < destSM.Metrics().Len(); i++ {
		m := destSM.Metrics().At(i)
		metricMap[m.Name()] = m
	}

	ms := srcSM.Metrics()
	for i := 0; i < ms.Len(); i++ {
		srcM := ms.At(i)
		name := srcM.Name()
		
		destM, ok := metricMap[name]
		if !ok {
			destM = destSM.Metrics().AppendEmpty()
			destM.SetName(name)
			destM.SetDescription(srcM.Description())
			destM.SetUnit(srcM.Unit())
			metricMap[name] = destM
		}
		
		p.rollupMetricData(destM, srcM, level)
	}
}

func (p *Processor) rollupMetricData(dest pmetric.Metric, src pmetric.Metric, level CardinalityLevel) {
	switch src.Type() {
	case pmetric.MetricTypeSum:
		if dest.Type() == pmetric.MetricTypeEmpty {
			srcSum := src.Sum()
			destSum := dest.SetEmptySum()
			destSum.SetIsMonotonic(srcSum.IsMonotonic())
			destSum.SetAggregationTemporality(srcSum.AggregationTemporality())
		}
		p.rollupDataPoints(dest.Sum().DataPoints(), src.Sum().DataPoints(), level)
	
	case pmetric.MetricTypeGauge:
		if dest.Type() == pmetric.MetricTypeEmpty {
			dest.SetEmptyGauge()
		}
		p.rollupDataPoints(dest.Gauge().DataPoints(), src.Gauge().DataPoints(), level)
		
	default:
		// Unsupported types in prototype: just copy first occurrence
		if dest.Type() == pmetric.MetricTypeEmpty {
			src.CopyTo(dest)
		}
	}
}

func (p *Processor) rollupDataPoints(dest pmetric.NumberDataPointSlice, src pmetric.NumberDataPointSlice, level CardinalityLevel) {
	// Group points by filtered attributes
	groups := make(map[string]pmetric.NumberDataPoint)
	
	// Index existing in dest
	for i := 0; i < dest.Len(); i++ {
		dp := dest.At(i)
		groups[p.attrKey(dp.Attributes())] = dp
	}

	for i := 0; i < src.Len(); i++ {
		srcDP := src.At(i)
		
		// Filtered key
		attrKey := p.filterAttributesKey(srcDP.Attributes(), level)
		
		if existing, ok := groups[attrKey]; ok {
			// Aggregate
			if srcDP.ValueType() == pmetric.NumberDataPointValueTypeInt {
				existing.SetIntValue(existing.IntValue() + srcDP.IntValue())
			} else {
				existing.SetDoubleValue(existing.DoubleValue() + srcDP.DoubleValue())
			}
		} else {
			newDP := dest.AppendEmpty()
			srcDP.CopyTo(newDP)
			newDP.Attributes().Clear()
			p.applyFilteredAttributes(newDP.Attributes(), srcDP.Attributes(), level)
			groups[attrKey] = newDP
		}
	}
}

func (p *Processor) attrKey(attrs pcommon.Map) string {
	var res string
	attrs.Range(func(k string, v pcommon.Value) bool {
		res += fmt.Sprintf("%s=%s;", k, v.AsString())
		return true
	})
	return res
}

func (p *Processor) filterAttributesKey(attrs pcommon.Map, level CardinalityLevel) string {
	if level == LevelCoarse {
		return ""
	}
	
	allowed := map[string]bool{
		"http.method": true,
		"http.status_code": true,
		"error": true,
	}
	
	var result string
	attrs.Range(func(k string, v pcommon.Value) bool {
		if allowed[k] {
			result += fmt.Sprintf("%s=%s;", k, v.AsString())
		}
		return true
	})
	return result
}

func (p *Processor) applyFilteredAttributes(dest pcommon.Map, src pcommon.Map, level CardinalityLevel) {
	if level == LevelCoarse {
		return
	}
	allowed := map[string]bool{
		"http.method": true,
		"http.status_code": true,
		"error": true,
	}
	src.Range(func(k string, v pcommon.Value) bool {
		if allowed[k] {
			v.CopyTo(dest.PutEmpty(k))
		}
		return true
	})
}

func (p *Processor) scopeKey(is pcommon.InstrumentationScope) string {
	return is.Name() + is.Version()
}

func (p *Processor) mergeMetrics(signals []*signals.MetricSignal) pmetric.Metrics {

	out := pmetric.NewMetrics()

	for _, sig := range signals {

		sig.Metrics.ResourceMetrics().MoveAndAppendTo(out.ResourceMetrics())

	}

	return out

}



// RollupLogs reduces log cardinality by grouping by severity and pattern

func (p *Processor) RollupLogs(signals []*signals.LogSignal) plog.Logs {

	p.mu.RLock()

	level := p.currentLevel

	p.mu.RUnlock()



	if level == LevelFine || !p.cfg.Enabled {

		return p.mergeLogs(signals)

	}



	out := plog.NewLogs()

	resMap := make(map[string]plog.ResourceLogs)



	for _, sig := range signals {

		rls := sig.Logs.ResourceLogs()

		for i := 0; i < rls.Len(); i++ {

			rl := rls.At(i)

			resKey := p.attrKey(rl.Resource().Attributes())

			

			destRL, ok := resMap[resKey]

			if !ok {

				destRL = out.ResourceLogs().AppendEmpty()

				rl.Resource().CopyTo(destRL.Resource())

				resMap[resKey] = destRL

			}

			

			p.mergeScopeLogs(destRL, rl, level)

		}

	}



	return out

}



func (p *Processor) mergeScopeLogs(destRL plog.ResourceLogs, srcRL plog.ResourceLogs, level CardinalityLevel) {

	scopeMap := make(map[string]plog.ScopeLogs)

	for i := 0; i < destRL.ScopeLogs().Len(); i++ {

		sl := destRL.ScopeLogs().At(i)

		scopeMap[p.scopeKey(sl.Scope())] = sl

	}



	sls := srcRL.ScopeLogs()

	for i := 0; i < sls.Len(); i++ {

		srcSL := sls.At(i)

		sk := p.scopeKey(srcSL.Scope())

		

		destSL, ok := scopeMap[sk]

		if !ok {

			destSL = destRL.ScopeLogs().AppendEmpty()

			srcSL.Scope().CopyTo(destSL.Scope())

			scopeMap[sk] = destSL

		}

		

		p.rollupLogRecords(destSL.LogRecords(), srcSL.LogRecords(), level)

	}

}



func (p *Processor) rollupLogRecords(dest plog.LogRecordSlice, src plog.LogRecordSlice, level CardinalityLevel) {

	// Group logs by pattern and severity

	groups := make(map[string]plog.LogRecord)

	

	// Index existing

	for i := 0; i < dest.Len(); i++ {

		lr := dest.At(i)

		groups[p.logKey(lr, level)] = lr

	}



	for i := 0; i < src.Len(); i++ {

		srcLR := src.At(i)

		key := p.logKey(srcLR, level)

		

		if existing, ok := groups[key]; ok {

			// In a rollup, we might want to count occurrences.

			// Standard OTEL logs don't have a "count" field by default, 

			// but we can add an attribute.

			countAttr, found := existing.Attributes().Get("rollup.count")

			count := int64(1)

			if found {

				count = countAttr.Int() + 1

			}

			existing.Attributes().PutInt("rollup.count", count)

		} else {

			newLR := dest.AppendEmpty()

			srcLR.CopyTo(newLR)

			newLR.Attributes().PutInt("rollup.count", 1)

			// Strip high cardinality attributes if level is coarse

			if level == LevelCoarse {

				newLR.Attributes().Remove("request_id")

				newLR.Attributes().Remove("user_id")

			}

			groups[key] = newLR

		}

	}

}



func (p *Processor) logKey(lr plog.LogRecord, level CardinalityLevel) string {

	// Pattern extraction would be complex. For prototype: severity + first 20 chars of body

	body := lr.Body().AsString()

	if len(body) > 20 && level == LevelCoarse {

		body = body[:20] + "..."

	}

	return fmt.Sprintf("%d:%s", lr.SeverityNumber(), body)

}



func (p *Processor) mergeLogs(signals []*signals.LogSignal) plog.Logs {

	out := plog.NewLogs()

	for _, sig := range signals {

		sig.Logs.ResourceLogs().MoveAndAppendTo(out.ResourceLogs())

	}

	return out

}
