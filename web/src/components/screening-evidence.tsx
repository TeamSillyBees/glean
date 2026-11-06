import type { ScreeningEvidence } from '../lib/types'
import { managementField } from './management-field'

const screeningCriteria = {
  identity: '身份',
  topic: '主题',
  method: '方法',
  date: '日期',
  venue: '载体',
  document_type: '文类',
  version: '版本',
} as const

export function ScreeningEditor({
  value,
  onChange,
}: {
  value: ScreeningEvidence
  onChange: (value: ScreeningEvidence) => void
}) {
  return (
    <details className="rounded-lg border p-3" open>
      <summary className="cursor-pointer text-sm font-medium">审读与分项证据</summary>
      <p className="my-3 text-xs leading-6 text-muted-foreground">
        取得摘要或全文不等于完成审读。纳入需各项证据满足；排除需至少一项明确否定。未知项请保留待核。
        当前协议：{value.profile_id} · {value.profile_version}
      </p>
      <label className="block text-sm">
        实际阅读层级
        <select
          aria-label="实际阅读层级"
          className={managementField}
          value={value.reading_level}
          onChange={(e) => onChange({ ...value, reading_level: e.target.value })}
        >
          <option value="metadata">仅题录</option>
          <option value="abstract_available">摘要已取得</option>
          <option value="abstract_reviewed">完整摘要已审读</option>
          <option value="fulltext_fragment_reviewed">正文片段已核读</option>
          <option value="fulltext_accessible">全文可访问</option>
          <option value="fulltext_reviewed">全文已审读</option>
        </select>
      </label>
      <label className="my-3 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={value.requires_fulltext}
          onChange={(e) => onChange({ ...value, requires_fulltext: e.target.checked })}
        />
        该判断需要全文核验
      </label>
      {(
        [
          ['identity_basis', '身份依据'],
          ['research_object', '研究对象'],
          ['method_role', '方法角色'],
          ['topic_role', '核心或邻近边界'],
        ] as const
      ).map(([key, label]) => (
        <label key={key} className="mb-3 block text-sm">
          {label}
          <input
            aria-label={label}
            className={managementField}
            value={value[key]}
            maxLength={2000}
            onChange={(e) => onChange({ ...value, [key]: e.target.value })}
          />
        </label>
      ))}
      <div className="space-y-3">
        {Object.entries(screeningCriteria).map(([key, label]) => {
          const criterion = value.criteria[key] ?? {
            decision: 'unknown',
            evidence: '',
            source_url: '',
            location: '',
          }
          const update = (patch: Partial<typeof criterion>) =>
            onChange({
              ...value,
              criteria: { ...value.criteria, [key]: { ...criterion, ...patch } },
            })
          return (
            <fieldset key={key} className="rounded border p-3">
              <legend className="px-1 text-sm">{label}证据</legend>
              <label className="block text-sm">
                {label}结论
                <select
                  aria-label={`${label}结论`}
                  className={managementField}
                  value={criterion.decision}
                  onChange={(e) =>
                    update({ decision: e.target.value as typeof criterion.decision })
                  }
                >
                  <option value="unknown">未知 / 待核</option>
                  <option value="pass">满足</option>
                  <option value="fail">明确不满足</option>
                  {key === 'venue' && <option value="not_applicable">未设置载体门槛</option>}
                </select>
              </label>
              {criterion.decision !== 'unknown' && (
                <div className="mt-2 space-y-2">
                  <label className="block text-sm">
                    {label}短证据
                    <textarea
                      aria-label={`${label}短证据`}
                      className={managementField}
                      required
                      maxLength={4000}
                      value={criterion.evidence}
                      onChange={(e) => update({ evidence: e.target.value })}
                    />
                  </label>
                  {criterion.decision !== 'not_applicable' && (
                    <>
                      <label className="block text-sm">
                        {label}主源链接
                        <input
                          aria-label={`${label}主源链接`}
                          className={managementField}
                          type="url"
                          required
                          maxLength={2000}
                          value={criterion.source_url}
                          onChange={(e) => update({ source_url: e.target.value })}
                        />
                      </label>
                      <label className="block text-sm">
                        {label}证据定位
                        <input
                          aria-label={`${label}证据定位`}
                          className={managementField}
                          required
                          maxLength={1000}
                          value={criterion.location}
                          onChange={(e) => update({ location: e.target.value })}
                          placeholder="摘要第几句 / 正文页码、表格或段落"
                        />
                      </label>
                    </>
                  )}
                </div>
              )}
            </fieldset>
          )
        })}
      </div>
    </details>
  )
}
