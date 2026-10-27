import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  ArrowRight,
  BookMarked,
  BookOpen,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Dna,
  EyeOff,
  FileCode2,
  FlaskConical,
  Layers3,
  LoaderCircle,
  LogOut,
  Menu,
  Network,
  Radio,
  RotateCcw,
  Save,
  Search,
  Settings2,
  ShieldAlert,
  SlidersHorizontal,
  Sparkles,
  X,
} from 'lucide-react'
import { api, setSessionExpired } from './lib/api'
import {
  defaultFilters,
  type Facet,
  type Facets,
  type Filters,
  type Identity,
  type Topic,
  type TopicSlug,
} from './lib/types'
import { filterFacets, getFilterSpecs, restoreFilters } from './lib/filters'
import { cn, number, readLabels, reviewLabels, topicLabels } from './lib/utils'
import { Badge, Empty, ErrorState, Loading } from './components/common'
import { Button } from './components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './components/ui/dialog'
import { Select } from './components/ui/select'
import { FilterCombobox } from './components/ui/filter-combobox'
import { Tooltip, TooltipProvider } from './components/ui/tooltip'
import { QuarantineView } from './components/quarantine'
import { PaperCard, PaperDetail } from './components/papers'
import {
  FreshnessStrip,
  RunsView,
  SourcesView,
  TemplatesView,
  VenuesView,
} from './components/monitor'

type View = 'papers' | 'sources' | 'runs' | 'templates' | 'venues' | 'quarantine'
const topicIcons = { 'biomed-ai': Dna, 'llm-agent': Network, 'biomed-worldmodel': Layers3 }
const viewMeta: Record<View, { title: string; description: string; eyebrow: string }> = {
  quarantine: {
    title: '隔离记录',
    description: '追溯隔离原因，核验安全证据。',
    eyebrow: 'READ-ONLY EVIDENCE',
  },
  papers: {
    title: '论文工作台',
    description: '从海量研究中，找到值得认真阅读的下一篇。',
    eyebrow: 'YOUR RESEARCH, IN FOCUS',
  },
  sources: {
    title: '来源健康',
    description: '每个来源各自前进，数据从哪里来始终清楚。',
    eyebrow: 'CONNECTED KNOWLEDGE',
  },
  runs: {
    title: '扫描记录',
    description: '采集、审核与目录投影，每一步都留下记录。',
    eyebrow: 'A TRACEABLE WORKFLOW',
  },
  templates: {
    title: '检索模板',
    description: '统一检索意图，保留各来源的语法与证据。',
    eyebrow: 'SEARCH WITH INTENTION',
  },
  venues: {
    title: '期刊与会议',
    description: '可追溯的指标，可核验的纳入依据。',
    eyebrow: 'KNOW YOUR SOURCES',
  },
}
function Logo({ compact = false }: { compact?: boolean }) {
  return (
    <div className="flex items-center gap-2.5">
      <span className="brand-symbol">
        <Sparkles className="size-5" strokeWidth={1.7} />
      </span>
      {!compact && (
        <span className="text-[26px] font-semibold tracking-[-1.3px] text-slate-900">
          glean<span className="text-primary">.</span>
        </span>
      )}
    </div>
  )
}
function Login({ onLogin, expired }: { onLogin: (user: Identity) => void; expired: boolean }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api.login(username.trim(), password),
    onSuccess: (user) => {
      setPassword('')
      onLogin(user)
    },
  })
  function submit(event: FormEvent) {
    event.preventDefault()
    if (username.trim() && password && !mutation.isPending) mutation.mutate()
  }
  return (
    <div className="login-layout">
      <section className="login-story">
        <Logo />
        <div className="relative z-10 my-auto max-w-xl py-20">
          <span className="mb-7 inline-flex items-center gap-2 rounded-full border border-violet-200 bg-white/60 px-3 py-1.5 text-xs text-violet-700">
            <span className="size-1.5 rounded-full bg-violet-500" />
            一个安静、专注的研究空间
          </span>
          <h1 className="text-4xl font-semibold leading-[1.6] tracking-tight xl:text-5xl">
            让下一篇重要论文，
            <br />
            <span className="text-primary">更早被看见。</span>
          </h1>
          <p className="mt-6 max-w-sm text-sm leading-8 text-slate-500">
            连接历史积累与新的发现。以清晰的主题、可追溯的来源和有序的阅读，保持研究的连续性。
          </p>
          <div className="mt-10 flex flex-wrap gap-3">
            {Object.entries(topicLabels).map(([slug, name]) => {
              const Icon = topicIcons[slug as TopicSlug]
              return (
                <span
                  key={slug}
                  className="flex items-center gap-2 rounded-lg border border-white/80 bg-white/65 px-3 py-2 text-xs text-slate-600"
                >
                  <Icon className="size-3.5 text-primary" />
                  {name}
                </span>
              )
            })}
          </div>
        </div>
        <div className="relative z-10 flex items-center gap-2 text-xs text-slate-400">
          <BookOpen className="size-3.5" />
          Keep the signal. Make room for discovery.
        </div>
        <div className="login-orbit orbit-one" />
        <div className="login-orbit orbit-two" />
      </section>
      <main className="flex items-center justify-center px-7 py-12 sm:px-12">
        <div className="w-full max-w-sm">
          <div className="mb-12 lg:hidden">
            <Logo />
          </div>
          <div className="mb-8">
            <p className="eyebrow">WELCOME TO GLEAN</p>
            <h2 className="mt-3 text-2xl font-semibold tracking-tight">回到你的研究工作台</h2>
            <p className="mt-3 text-sm leading-7 text-muted-foreground">
              使用后端账户登录，继续检索与阅读。
            </p>
          </div>
          {expired && (
            <p
              role="status"
              className="mb-5 rounded-lg bg-amber-50 p-3 text-xs leading-6 text-amber-800"
            >
              会话已结束，请重新登录。你的阅读状态已保存在后端。
            </p>
          )}
          <form onSubmit={submit} className="space-y-5">
            <label className="block text-sm font-medium">
              用户名
              <input
                className="form-input mt-2"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="输入你的用户名"
                required
                disabled={mutation.isPending}
              />
            </label>
            <label className="block text-sm font-medium">
              密码
              <input
                className="form-input mt-2"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="输入账户密码"
                required
                disabled={mutation.isPending}
              />
            </label>
            {mutation.isError && (
              <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm leading-6 text-red-700">
                {mutation.error.message}
              </p>
            )}
            <Button className="h-11 w-full" disabled={mutation.isPending} type="submit">
              {mutation.isPending ? (
                <>
                  <LoaderCircle className="animate-spin" />
                  正在连接
                </>
              ) : (
                <>
                  进入工作台
                  <ArrowRight />
                </>
              )}
            </Button>
          </form>
          <p className="mt-7 text-xs leading-6 text-muted-foreground">
            访问令牌仅保留在本次页面内存中。此界面只连接 Glean 后端，不直接调用外部检索或模型服务。
          </p>
          <div className="mt-10 border-t pt-5">
            <span className="inline-flex items-center gap-2 text-xs text-slate-400">
              <CircleHelp className="size-3.5" />
              首次使用？请先完成后端本地账户初始化。
            </span>
          </div>
        </div>
      </main>
    </div>
  )
}
function FilterPanel({
  filters,
  facets,
  onChange,
  onReset,
  onSave,
  saving,
  loading,
  failed,
  retry,
}: {
  filters: Filters
  facets?: Facets
  onChange: (key: keyof Filters, value: string) => void
  onReset: () => void
  onSave: () => void
  saving: boolean
  loading: boolean
  failed: boolean
  retry: () => void
}) {
  const filterSpecs = getFilterSpecs(filters.topic)
  return (
    <div
      data-testid={loading ? 'filters-loading' : failed ? 'filters-error' : 'filters-ready'}
      aria-busy={loading}
      className="rounded-xl border bg-white"
    >
      <div className="flex items-center justify-between border-b px-5 py-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <SlidersHorizontal className="size-4" />
          筛选条件
        </h2>
        <button
          onClick={onReset}
          className="rounded text-xs text-muted-foreground hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          重置
        </button>
      </div>
      {loading && (
        <p
          role="status"
          className="flex items-center gap-2 px-5 pt-4 text-xs text-muted-foreground"
        >
          <LoaderCircle className="size-3.5 animate-spin" />
          正在读取本目录筛选项…
        </p>
      )}
      {failed && (
        <div role="alert" className="px-5 pt-4 text-xs text-amber-800">
          筛选项读取失败。
          <button onClick={retry} className="ml-2 rounded text-primary underline">
            重新读取
          </button>
        </div>
      )}
      <div className="space-y-3 px-5 py-4">
        {filterSpecs.map((spec) => {
          let values: Facet[] = filterFacets(facets, filters.topic, spec.key, spec.facet)
          if (spec.key === 'readStatus' && !values.length)
            values = Object.keys(readLabels).map((value) => ({ value, count: 0 }))
          const selected = String(filters[spec.key])
          const hasSelected = values.some((item) => item.value === selected)
          if (selected && !hasSelected) values = [{ value: selected, count: 0 }, ...values]
          return (
            <div key={spec.key}>
              <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-slate-500">
                <span>{spec.label}</span>
                <Tooltip label={spec.help}>
                  <button
                    type="button"
                    aria-label={`${spec.label}说明`}
                    className="rounded text-slate-300 hover:text-primary"
                  >
                    <CircleHelp className="size-3" />
                  </button>
                </Tooltip>
              </div>
              <FilterCombobox
                disabled={loading || failed}
                filterKey={spec.key}
                label={spec.label}
                value={selected}
                onValueChange={(value) => onChange(spec.key, value)}
                placeholder={
                  loading
                    ? '筛选项加载中'
                    : failed
                      ? '筛选项读取失败'
                      : values.length
                        ? '全部'
                        : '当前目录暂无此标签'
                }
                options={values.map((item) => ({
                  value: item.value,
                  label:
                    spec.key === 'readStatus'
                      ? readLabels[item.value] || item.value
                      : spec.key === 'reviewStatus'
                        ? reviewLabels[item.value] || item.value
                        : item.value,
                  count: selected === item.value && !hasSelected ? undefined : item.count,
                }))}
              />
            </div>
          )
        })}
      </div>
      <div className="border-t p-4">
        <Button variant="outline" className="w-full" size="sm" onClick={onSave} disabled={saving}>
          {saving ? <LoaderCircle className="animate-spin" /> : <Save />}保存当前筛选
        </Button>
        <p className="mt-3 text-center text-[10px] text-muted-foreground">
          条件之间取交集 · 标签保留原目录含义
        </p>
      </div>
    </div>
  )
}
function Workspace({ user, onLogout }: { user: Identity; onLogout: () => void }) {
  const prefs = useQuery({
    queryKey: ['preferences'],
    queryFn: ({ signal }) => api.preferences(signal),
  })
  if (prefs.isLoading) return <Loading label="正在恢复你的阅读偏好…" />
  if (prefs.isError)
    return (
      <main className="mx-auto max-w-xl p-10">
        <ErrorState error={prefs.error} retry={() => prefs.refetch()} />
      </main>
    )
  return (
    <WorkspaceReady
      user={user}
      onLogout={onLogout}
      initialFilters={{ ...defaultFilters, ...restoreFilters(prefs.data?.filters || {}) }}
    />
  )
}
function WorkspaceReady({
  user,
  onLogout,
  initialFilters,
}: {
  user: Identity
  onLogout: () => void
  initialFilters: Filters
}) {
  const client = useQueryClient()
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [view, setView] = useState<View>('papers')
  const [quarantineSource, setQuarantineSource] = useState('')
  const [search, setSearch] = useState(initialFilters.q)
  const [venueSearch, setVenueSearch] = useState('')
  const [venueQuery, setVenueQuery] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [mobileNav, setMobileNav] = useState(false)
  const [mobileFilters, setMobileFilters] = useState(false)
  const [settings, setSettings] = useState(false)
  const [message, setMessage] = useState('')
  const searchRef = useRef<HTMLInputElement>(null)
  const resultsRef = useRef<HTMLElement>(null)
  const detailTrigger = useRef<HTMLElement | null>(null)
  const prefs = useQuery({
    queryKey: ['preferences'],
    queryFn: ({ signal }) => api.preferences(signal),
  })
  const topics = useQuery({ queryKey: ['topics'], queryFn: ({ signal }) => api.topics(signal) })
  useEffect(() => {
    if (!message) return
    const timeout = setTimeout(() => setMessage(''), 4500)
    return () => clearTimeout(timeout)
  }, [message])
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        searchRef.current?.focus()
      }
    }
    document.addEventListener('keydown', keydown)
    return () => document.removeEventListener('keydown', keydown)
  }, [])
  const paperQuery = useQuery({
    queryKey: ['papers', filters],
    queryFn: ({ signal }) => api.papers(filters, signal),
    enabled: view === 'papers',
  })
  const lastPage = Math.max(1, Math.ceil((paperQuery.data?.total ?? 0) / filters.pageSize))
  if (
    view === 'papers' &&
    paperQuery.isSuccess &&
    !paperQuery.isFetching &&
    filters.page > lastPage
  ) {
    setFilters({ ...filters, page: lastPage })
  }
  const facetQuery = useQuery({
    queryKey: ['facets', filters.topic, filters.hidden, filters.collectionScope],
    queryFn: ({ signal }) =>
      api.facets(
        {
          ...defaultFilters,
          topic: filters.topic,
          hidden: filters.hidden,
          collectionScope: filters.collectionScope,
        },
        signal,
      ),
    enabled: view === 'papers',
  })
  const savePrefs = useMutation({
    mutationFn: () => api.savePreferences({ ...prefs.data?.filters, ...filters, page: 1 }),
    onSuccess: (data) => {
      client.setQueryData(['preferences'], data)
      setMessage('筛选偏好已保存，下次登录时会自动恢复。')
    },
    onError: (error) => setMessage(`保存失败：${error.message}`),
  })
  const logout = useMutation({
    mutationFn: () => api.logout(),
    onMutate: () => {
      client.clear()
      onLogout()
    },
  })
  const activeTopic = topics.data?.items?.find((topic) => topic.slug === filters.topic)
  const filterSpecs = getFilterSpecs(filters.topic)
  const activeFilters = filterSpecs.filter((spec) => filters[spec.key])
  const activeCount =
    activeFilters.length +
    [
      filters.q,
      filters.readStatus,
      filters.source,
      filters.inclusionStatus,
      filters.reviewStatus,
    ].filter(Boolean).length
  const reset = () => {
    setFilters({ ...defaultFilters, topic: filters.topic, hidden: filters.hidden })
    setSearch('')
  }
  const change = (key: keyof Filters, value: string) =>
    setFilters((old) => ({ ...old, [key]: value, page: 1 }))
  const navigate = (next: View) => {
    setView(next)
    setMobileNav(false)
    setSelected(null)
  }
  const chooseTopic = (topic: TopicSlug) => {
    setFilters({ ...defaultFilters, topic })
    setSearch('')
    setMobileNav(false)
    setSelected(null)
  }
  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    if (view === 'venues') setVenueQuery(venueSearch.trim())
    else change('q', search.trim())
  }
  const refresh = () => {
    client.invalidateQueries({ queryKey: [view === 'papers' ? 'papers' : view] })
    if (view === 'papers') {
      client.invalidateQueries({ queryKey: ['facets'] })
      client.invalidateQueries({ queryKey: ['topics'] })
    }
  }
  const navigation = (
    <>
      <div className="mb-9 px-2">
        <Logo />
      </div>
      <div className="space-y-1">
        <NavButton
          icon={BookOpen}
          active={view === 'papers' && filters.hidden !== 'only'}
          onClick={() => {
            change('hidden', 'exclude')
            navigate('papers')
          }}
        >
          论文工作台
        </NavButton>
        <NavButton
          icon={EyeOff}
          active={view === 'papers' && filters.hidden === 'only'}
          onClick={() => {
            setFilters((old) => ({ ...old, hidden: 'only', collectionScope: 'all', page: 1 }))
            navigate('papers')
          }}
        >
          已隐藏
        </NavButton>
      </div>
      <div className="mb-3 mt-8 px-3 text-[10px] font-semibold tracking-[.14em] text-slate-400">
        我的研究主题
      </div>
      <div className="space-y-1">
        {Object.entries(topicLabels).map(([slug, label]) => {
          const Icon = topicIcons[slug as TopicSlug]
          const topic = topics.data?.items?.find((item) => item.slug === slug)
          return (
            <button
              key={slug}
              onClick={() => chooseTopic(slug as TopicSlug)}
              className={cn('topic-button', filters.topic === slug && 'active')}
            >
              <span className="topic-icon">
                <Icon className="size-4" />
              </span>
              <span className="flex-1 text-left text-xs font-medium">{label}</span>
              <span className="text-[10px] tabular-nums opacity-60">
                {number(topic?.membershipCount)}
              </span>
            </button>
          )
        })}
      </div>
      <div className="mb-3 mt-8 px-3 text-[10px] font-semibold tracking-[.14em] text-slate-400">
        监测与来源
      </div>
      <div className="space-y-1">
        <NavButton icon={Radio} active={view === 'sources'} onClick={() => navigate('sources')}>
          来源健康
        </NavButton>
        <NavButton
          icon={ShieldAlert}
          active={view === 'quarantine'}
          onClick={() => {
            setQuarantineSource('')
            navigate('quarantine')
          }}
        >
          隔离记录
        </NavButton>
        <NavButton icon={Activity} active={view === 'runs'} onClick={() => navigate('runs')}>
          扫描记录
        </NavButton>
        <NavButton
          icon={FileCode2}
          active={view === 'templates'}
          onClick={() => navigate('templates')}
        >
          检索模板
        </NavButton>
        <NavButton icon={BookMarked} active={view === 'venues'} onClick={() => navigate('venues')}>
          期刊与会议
        </NavButton>
      </div>
      <div className="mt-auto pt-10">
        <div className="mb-5 rounded-xl border border-violet-100 bg-gradient-to-br from-violet-50 to-white p-4">
          <FlaskConical className="mb-2.5 size-4 text-primary" />
          <p className="text-xs font-medium text-slate-700">让研究，保持连贯。</p>
          <p className="mt-2 text-[11px] leading-5 text-slate-400">
            历史积累与每一次新发现，
            <br />
            都在同一个地方。
          </p>
        </div>
        <button
          onClick={() => {
            setSettings(true)
            setMobileNav(false)
          }}
          className="flex w-full items-center gap-3 rounded-lg p-2 text-left hover:bg-secondary"
        >
          <span className="flex size-8 items-center justify-center rounded-full bg-violet-100 text-xs font-semibold text-violet-700">
            {(user.nickname || user.username).slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-xs font-medium">
              {user.nickname || user.username}
            </span>
            <span className="text-[10px] text-slate-400">个人研究空间</span>
          </span>
          <Settings2 className="size-4 text-slate-400" />
        </button>
      </div>
    </>
  )
  return (
    <div className="app-layout">
      <a href="#main-content" className="skip-link">
        跳到主要内容
      </a>
      <aside className="desktop-sidebar">{navigation}</aside>
      <div className="workspace">
        <header className="topbar">
          <Button
            className="lg:hidden"
            variant="ghost"
            size="icon"
            aria-label="打开导航"
            onClick={() => setMobileNav(true)}
          >
            <Menu />
          </Button>
          <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
            <span className="hidden sm:inline">研究空间</span>
            <ChevronRight className="hidden size-3 sm:inline" />
            <span className="truncate font-medium text-slate-600">
              {topicLabels[filters.topic]}
            </span>
          </div>
          <span className="ml-auto flex items-center gap-1.5 text-[11px] text-slate-400">
            <span className="size-1.5 rounded-full bg-emerald-500" />
            已登录
          </span>
          <div className="ml-2 h-4 w-px bg-border" />
          <Tooltip label="刷新当前视图">
            <Button variant="ghost" size="icon" onClick={refresh} aria-label="刷新当前视图">
              <RotateCcw
                className={cn((paperQuery.isFetching || topics.isFetching) && 'animate-spin')}
              />
            </Button>
          </Tooltip>
        </header>
        <main id="main-content" tabIndex={-1} className="main-content outline-none">
          <div className="mb-4 flex flex-wrap items-end justify-between gap-3 sm:mb-7 sm:gap-5">
            <div>
              <p className="eyebrow hidden sm:block">{viewMeta[view].eyebrow}</p>
              <h1 className="text-[21px] font-semibold tracking-tight text-slate-900 sm:mt-2 sm:text-[28px]">
                {view === 'papers' && filters.hidden === 'only'
                  ? '已隐藏的论文'
                  : viewMeta[view].title}
              </h1>
              <p className="mt-2 hidden text-[13px] leading-6 text-muted-foreground sm:block">
                {view === 'papers' && filters.hidden === 'only'
                  ? '这里保存你在所有主题中隐藏的论文，随时可以恢复。'
                  : viewMeta[view].description}
              </p>
            </div>
            <div className="hidden items-center gap-2 rounded-lg border bg-white px-3 py-2.5 text-xs text-slate-600 sm:flex">
              <span className="size-1.5 rounded-full bg-primary" />
              {topicLabels[filters.topic]}
            </div>
          </div>
          {view === 'papers' && (
            <>
              <TopicIntro topic={activeTopic} loading={topics.isLoading} />
              <FreshnessStrip topic={filters.topic} onOpen={() => navigate('sources')} />
            </>
          )}
          {(view === 'papers' || view === 'venues') && (
            <form onSubmit={submitSearch} className="search-bar mb-6">
              <Search className="size-[18px] shrink-0 text-slate-400" />
              <input
                ref={searchRef}
                value={view === 'venues' ? venueSearch : search}
                maxLength={300}
                onChange={(event) =>
                  view === 'venues'
                    ? setVenueSearch(event.target.value)
                    : setSearch(event.target.value)
                }
                placeholder={view === 'venues' ? '搜索期刊或会议名称…' : '搜索论文标题、摘要、DOI…'}
                aria-label={view === 'venues' ? '搜索期刊或会议' : '搜索论文或来源'}
                className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-slate-400"
              />
              {(view === 'venues' ? venueSearch : search) && (
                <button
                  type="button"
                  aria-label="清空搜索"
                  onClick={() => {
                    if (view === 'venues') {
                      setVenueSearch('')
                      setVenueQuery('')
                    } else {
                      setSearch('')
                      change('q', '')
                    }
                  }}
                  className="rounded p-1 hover:bg-secondary"
                >
                  <X className="size-3.5 text-slate-400" />
                </button>
              )}
              <kbd className="hidden rounded border bg-secondary/60 px-1.5 py-1 text-[10px] text-slate-400 sm:block">
                ⌘ / Ctrl K
              </kbd>
              <Button size="sm" type="submit">
                搜索
              </Button>
            </form>
          )}
          {prefs.isError ? (
            <ErrorState error={prefs.error} retry={() => prefs.refetch()} />
          ) : view === 'papers' ? (
            <div className="papers-layout">
              <aside className="hidden xl:block">
                <FilterPanel
                  filters={filters}
                  facets={facetQuery.data}
                  loading={facetQuery.isPending}
                  failed={facetQuery.isError}
                  retry={() => facetQuery.refetch()}
                  onChange={change}
                  onReset={reset}
                  onSave={() => savePrefs.mutate()}
                  saving={savePrefs.isPending}
                />
              </aside>
              <section
                ref={resultsRef}
                className="min-w-0 scroll-mt-5"
                aria-busy={paperQuery.isFetching}
              >
                <div className="mb-4 flex flex-wrap items-center gap-3">
                  <h2 data-testid="results-count" className="text-sm font-medium text-slate-600">
                    找到{' '}
                    <strong className="mx-1 text-base text-slate-900">
                      {number(paperQuery.data?.total)}
                    </strong>{' '}
                    篇论文
                  </h2>
                  {paperQuery.isFetching && (
                    <LoaderCircle
                      aria-label="正在刷新"
                      className="size-3.5 animate-spin text-primary"
                    />
                  )}
                  <Button
                    variant="outline"
                    size="sm"
                    className="ml-auto xl:hidden"
                    onClick={() => setMobileFilters(true)}
                  >
                    <SlidersHorizontal />
                    筛选{activeCount > 0 && <Badge tone="violet">{activeCount}</Badge>}
                  </Button>
                  <div className="ml-auto flex gap-2">
                    <div className="w-28">
                      <Select
                        label="个人阅读状态"
                        value={filters.readStatus}
                        onValueChange={(value) => change('readStatus', value)}
                        placeholder="全部阅读"
                        options={Object.entries(readLabels).map(([value, label]) => ({
                          value,
                          label,
                        }))}
                      />
                    </div>
                    <div className="w-32">
                      <Select
                        label="排序方式"
                        value={filters.sort}
                        onValueChange={(value) => change('sort', value || 'newest')}
                        placeholder="排序方式"
                        options={[
                          { value: 'newest', label: '最新发表优先' },
                          { value: 'observed', label: '最近来源变化' },
                          { value: 'source-updated', label: '来源最近更新' },
                          { value: 'oldest', label: '最早发表优先' },
                          { value: 'title', label: '标题顺序' },
                        ]}
                      />
                    </div>
                  </div>
                </div>
                <div className="mb-4 flex flex-wrap items-center gap-2 rounded-lg border bg-white px-3 py-2.5">
                  <div
                    role="group"
                    aria-label="目录范围"
                    className="flex w-full gap-1 rounded-md bg-secondary/70 p-1 sm:w-auto"
                  >
                    {(
                      [
                        { value: 'curated', label: '精选目录' },
                        { value: 'pending', label: '新发现待审' },
                        { value: 'all', label: '全部记录' },
                      ] as const
                    ).map((scope) => (
                      <button
                        key={scope.value}
                        type="button"
                        aria-pressed={filters.collectionScope === scope.value}
                        onClick={() =>
                          setFilters((old) => ({
                            ...old,
                            collectionScope: scope.value,
                            inclusionStatus: '',
                            page: 1,
                          }))
                        }
                        className={cn(
                          'flex-1 whitespace-nowrap rounded px-3 py-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring',
                          filters.collectionScope === scope.value
                            ? 'bg-white font-semibold text-primary shadow-sm'
                            : 'text-muted-foreground hover:text-foreground',
                        )}
                      >
                        {scope.label}
                      </button>
                    ))}
                  </div>
                  <div className="min-w-36 flex-1 sm:max-w-56">
                    <FilterCombobox
                      disabled={facetQuery.isPending || facetQuery.isError}
                      label="采集来源"
                      value={filters.source}
                      onValueChange={(value) => change('source', value)}
                      placeholder={
                        facetQuery.isPending
                          ? '来源加载中'
                          : facetQuery.isError
                            ? '来源读取失败'
                            : '全部来源'
                      }
                      options={(facetQuery.data?.sources || []).map((item) => ({
                        ...item,
                        label: item.value,
                      }))}
                    />
                  </div>
                </div>
                <p className="mb-4 text-[11px] leading-5 text-muted-foreground">
                  {filters.collectionScope === 'curated'
                    ? '沿用原目录的正式纳入范围，不将新发现自动纳入。'
                    : filters.collectionScope === 'pending'
                      ? '真实来源的新发现，尚未完成科学审读。'
                      : '全部记录包含精选、待核、背景与范围外资料。'}
                </p>
                {activeCount > 0 && (
                  <div className="mb-4 flex flex-wrap items-center gap-2">
                    {filters.q && (
                      <FilterChip
                        label={`搜索：${filters.q}`}
                        onRemove={() => {
                          change('q', '')
                          setSearch('')
                        }}
                      />
                    )}
                    {filters.readStatus && (
                      <FilterChip
                        label={`个人阅读：${readLabels[filters.readStatus]}`}
                        onRemove={() => change('readStatus', '')}
                      />
                    )}
                    {filters.source && (
                      <FilterChip
                        label={`采集来源：${filters.source}`}
                        onRemove={() => change('source', '')}
                      />
                    )}
                    {filters.inclusionStatus && (
                      <FilterChip
                        label={`记录范围：${filters.inclusionStatus === 'historical' ? '历史目录' : filters.inclusionStatus === 'pending-review' ? '新发现 · 待审' : filters.inclusionStatus}`}
                        onRemove={() => change('inclusionStatus', '')}
                      />
                    )}
                    {filters.reviewStatus && (
                      <FilterChip
                        label={`审读证据：${reviewLabels[filters.reviewStatus] || filters.reviewStatus}`}
                        onRemove={() => change('reviewStatus', '')}
                      />
                    )}
                    {activeFilters.map((spec) => (
                      <FilterChip
                        key={spec.key}
                        label={`${spec.label}：${spec.key === 'readStatus' ? readLabels[String(filters[spec.key])] : spec.key === 'reviewStatus' ? reviewLabels[String(filters[spec.key])] || filters[spec.key] : filters[spec.key]}`}
                        onRemove={() => change(spec.key, '')}
                      />
                    ))}
                    <button
                      onClick={reset}
                      className="p-1 text-[11px] text-muted-foreground hover:text-primary"
                    >
                      清除全部
                    </button>
                  </div>
                )}
                {facetQuery.isError && (
                  <p role="alert" className="mb-3 text-xs text-amber-800">
                    分类选项暂时不可用。
                    <button className="ml-2 underline" onClick={() => facetQuery.refetch()}>
                      重试
                    </button>
                  </p>
                )}
                {paperQuery.isLoading ? (
                  <div className="space-y-4" role="status" aria-label="正在载入论文">
                    {[1, 2, 3].map((item) => (
                      <div className="h-52 animate-pulse rounded-xl border bg-white p-6" key={item}>
                        <div className="h-3 w-1/4 rounded bg-secondary" />
                        <div className="mt-6 h-5 w-4/5 rounded bg-secondary" />
                        <div className="mt-4 h-3 w-full rounded bg-secondary" />
                        <div className="mt-2 h-3 w-3/4 rounded bg-secondary" />
                      </div>
                    ))}
                  </div>
                ) : paperQuery.isError ? (
                  <ErrorState error={paperQuery.error} retry={() => paperQuery.refetch()} />
                ) : !paperQuery.data?.items?.length ? (
                  <Empty
                    title={
                      filters.collectionScope === 'pending' && activeCount === 0
                        ? '还没有待审的新发现'
                        : undefined
                    }
                    description={
                      filters.collectionScope === 'pending' && activeCount === 0
                        ? '真实来源完成新发现采集后会显示在这里。历史待核资料可在「全部记录」中查看。'
                        : filters.hidden === 'only' && activeCount === 0
                          ? '当前主题暂无隐藏记录。你可以在论文详情里管理可见性。'
                          : '试试减少筛选条件，或换一个关键词。'
                    }
                  >
                    <Button variant="outline" size="sm" onClick={reset}>
                      {activeCount > 0 ? '清除筛选' : '返回精选目录'}
                    </Button>
                  </Empty>
                ) : (
                  <>
                    <div className="space-y-3.5">
                      {paperQuery.data.items.map((paper) => (
                        <PaperCard
                          key={paper.id}
                          paper={paper}
                          topic={filters.topic}
                          onOpen={() => {
                            detailTrigger.current = document.activeElement as HTMLElement
                            setSelected(paper.id)
                          }}
                        />
                      ))}
                    </div>
                    <div className="mt-7 flex flex-wrap items-center justify-between gap-4">
                      <p className="text-xs text-muted-foreground">
                        第 {number((filters.page - 1) * filters.pageSize + 1)}–
                        {number(Math.min(filters.page * filters.pageSize, paperQuery.data.total))}{' '}
                        篇，共 {number(paperQuery.data.total)} 篇
                      </p>
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="mr-2 w-28">
                          <Select
                            label="每页条数"
                            value={String(filters.pageSize)}
                            onValueChange={(value) =>
                              setFilters((old) => ({
                                ...old,
                                page: 1,
                                pageSize: Number(value) || 20,
                              }))
                            }
                            placeholder="每页条数"
                            options={[20, 40, 100].map((value) => ({
                              value: String(value),
                              label: `每页 ${value} 篇`,
                            }))}
                          />
                        </div>
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="上一页"
                          disabled={filters.page <= 1 || paperQuery.isFetching}
                          onClick={() => {
                            setFilters((old) => ({ ...old, page: old.page - 1 }))
                            resultsRef.current?.scrollIntoView({ block: 'start' })
                          }}
                        >
                          <ChevronLeft />
                        </Button>
                        <span className="min-w-14 text-center text-xs text-muted-foreground">
                          {filters.page} /{' '}
                          {Math.max(1, Math.ceil(paperQuery.data.total / filters.pageSize))}
                        </span>
                        <Button
                          variant="outline"
                          size="icon"
                          aria-label="下一页"
                          disabled={
                            filters.page * filters.pageSize >= paperQuery.data.total ||
                            paperQuery.isFetching
                          }
                          onClick={() => {
                            setFilters((old) => ({ ...old, page: old.page + 1 }))
                            resultsRef.current?.scrollIntoView({ block: 'start' })
                          }}
                        >
                          <ChevronRight />
                        </Button>
                      </div>
                    </div>
                  </>
                )}
              </section>
            </div>
          ) : view === 'sources' ? (
            <SourcesView
              topic={filters.topic}
              onOpenQuarantine={(source = '') => {
                setQuarantineSource(source)
                navigate('quarantine')
              }}
            />
          ) : view === 'quarantine' ? (
            <QuarantineView key={quarantineSource} initialSource={quarantineSource} />
          ) : view === 'runs' ? (
            <RunsView />
          ) : view === 'templates' ? (
            <TemplatesView topic={filters.topic} />
          ) : (
            <VenuesView topic={filters.topic} q={venueQuery} />
          )}
          <footer className="mt-10 flex flex-wrap items-center gap-2 border-t pt-5 text-[10px] text-slate-400">
            <Sparkles className="size-3" />
            <span>Glean · 为认真阅读留出空间</span>
            <span className="ml-auto">目录与指标均由独立后端提供</span>
          </footer>
        </main>
      </div>
      <PaperDetail
        restoreFocus={() =>
          detailTrigger.current?.isConnected
            ? detailTrigger.current.focus()
            : searchRef.current?.focus()
        }
        id={selected}
        onClose={() => setSelected(null)}
        notify={setMessage}
      />
      <Dialog open={mobileNav} onOpenChange={setMobileNav}>
        <DialogContent drawer className="left-0 right-auto max-w-[270px] p-5">
          <DialogTitle className="sr-only">导航</DialogTitle>
          <DialogDescription className="sr-only">研究主题与来源导航</DialogDescription>
          <div className="flex min-h-full flex-col pt-5">{navigation}</div>
        </DialogContent>
      </Dialog>
      <Dialog open={mobileFilters} onOpenChange={setMobileFilters}>
        <DialogContent drawer className="max-w-sm p-5">
          <DialogTitle className="mb-5 text-lg font-semibold">筛选论文</DialogTitle>
          <DialogDescription className="sr-only">按主题分类、来源和阅读状态筛选</DialogDescription>
          <FilterPanel
            filters={filters}
            facets={facetQuery.data}
            loading={facetQuery.isPending}
            failed={facetQuery.isError}
            retry={() => facetQuery.refetch()}
            onChange={change}
            onReset={reset}
            onSave={() => savePrefs.mutate()}
            saving={savePrefs.isPending}
          />
          <Button className="mt-5 w-full" onClick={() => setMobileFilters(false)}>
            查看结果
            <ArrowRight />
          </Button>
        </DialogContent>
      </Dialog>
      <Dialog open={settings} onOpenChange={setSettings}>
        <DialogContent>
          <DialogTitle className="text-lg font-semibold">你的研究空间</DialogTitle>
          <DialogDescription className="mt-2 text-sm leading-6 text-muted-foreground">
            阅读状态、隐藏列表与筛选偏好，均绑定当前账户。
          </DialogDescription>
          <div className="my-6 rounded-xl bg-secondary/60 p-4">
            <p className="text-sm font-medium">{user.nickname || user.username}</p>
            <p className="mt-1 text-xs text-muted-foreground">{user.username}</p>
          </div>
          <div className="space-y-3">
            <Button
              className="w-full justify-start"
              variant="outline"
              onClick={() => savePrefs.mutate()}
              disabled={savePrefs.isPending}
            >
              <Save />
              保存当前筛选偏好
            </Button>
            <Button
              className="w-full justify-start"
              variant="outline"
              onClick={() => {
                setSettings(false)
                setFilters((old) => ({ ...old, hidden: 'only', collectionScope: 'all', page: 1 }))
                navigate('papers')
              }}
            >
              <EyeOff />
              管理已隐藏论文
            </Button>
            <Button
              className="w-full justify-start text-muted-foreground"
              variant="ghost"
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
            >
              <LogOut />
              {logout.isPending ? '正在退出…' : '退出登录'}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
      {message && (
        <div
          role="status"
          className="fixed bottom-6 left-1/2 z-[80] flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-3 rounded-xl border bg-slate-900 px-5 py-3.5 text-sm text-white shadow-xl"
        >
          <Check className="size-4 shrink-0 text-violet-300" />
          <span>{message}</span>
          <button onClick={() => setMessage('')} aria-label="关闭提示" className="ml-2">
            <X className="size-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
function TopicIntro({ topic, loading }: { topic?: Topic; loading: boolean }) {
  return (
    <>
      <div className="mb-3 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground sm:hidden">
        主题共 {number(topic?.membershipCount)} 条记录
        {topic?.curatedCount != null && <span>· 精选 {number(topic.curatedCount)} 篇</span>}
      </div>
      <section className="topic-intro mb-6 hidden sm:block">
        <div className="relative z-10 flex flex-wrap items-center gap-4">
          <span className="flex size-11 shrink-0 items-center justify-center rounded-xl border border-white/80 bg-white/70 shadow-sm">
            <FlaskConical className="size-5 text-violet-600" />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="text-[13px] font-semibold text-slate-700">
              {topic?.description || '你的研究，全景呈现'}
            </h2>
            <p className="mt-1.5 text-[11px] leading-5 text-slate-500">
              {topic?.rule ||
                (loading ? '正在读取主题纳入规则…' : '纳入规则以独立后端的主题配置为准。')}
            </p>
          </div>
          <div className="border-l border-violet-200/70 pl-5 text-right">
            <span className="text-[23px] font-semibold tracking-tight text-violet-900">
              {number(topic?.membershipCount)}
            </span>
            <p className="mt-0.5 text-[10px] text-violet-700/60">主题目录条目</p>
            {topic?.historicalCount != null && (
              <p className="mt-1 text-[10px] text-slate-500">
                历史 {number(topic.historicalCount)}
                {topic.candidateCount != null && ` · 新发现待审 ${number(topic.candidateCount)}`}
              </p>
            )}
          </div>
        </div>
      </section>
    </>
  )
}
function NavButton({
  icon: Icon,
  active,
  onClick,
  children,
}: {
  icon: typeof BookOpen
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={cn('nav-button', active && 'active')}
      aria-current={active ? 'page' : undefined}
    >
      <Icon className="size-4" />
      {children}
      {active && <span className="ml-auto size-1.5 rounded-full bg-primary" />}
    </button>
  )
}
function FilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex max-w-full items-center gap-1.5 rounded-md border border-violet-100 bg-violet-50 px-2.5 py-1 text-[11px] text-violet-700">
      <span className="truncate">{label}</span>
      <button
        aria-label={`移除 ${label}`}
        onClick={onRemove}
        className="rounded p-0.5 hover:bg-violet-100"
      >
        <X className="size-3" />
      </button>
    </span>
  )
}
export default function App() {
  const [user, setUser] = useState<Identity | null>(null)
  const [expired, setExpired] = useState(false)
  const client = useQueryClient()
  useEffect(
    () =>
      setSessionExpired(() => {
        setUser(null)
        setExpired(true)
        client.clear()
      }),
    [client],
  )
  return (
    <TooltipProvider>
      {user ? (
        <Workspace
          user={user}
          onLogout={() => {
            setUser(null)
            setExpired(false)
          }}
        />
      ) : (
        <Login
          expired={expired}
          onLogin={(identity) => {
            setUser(identity)
            setExpired(false)
          }}
        />
      )}
    </TooltipProvider>
  )
}
