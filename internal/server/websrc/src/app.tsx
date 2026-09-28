import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import {
  IconAlertCircle,
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconCode,
  IconCopy,
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconLayoutSidebarRightCollapse,
  IconLayoutSidebarRightExpand,
  IconMenu2,
  IconMessageCircle,
  IconGitBranch,
  IconRefresh,
  IconSearch,
  IconSparkles,
  IconTerminal2,
  IconTrash,
  IconX,
} from '@tabler/icons-react'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { Input } from './components/ui/input'
import { PanelResize } from './components/panel-resize'
import { MarkdownContent } from './components/markdown'
import { Skeleton } from './components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from './components/ui/alert-dialog'
import { Tooltip } from './components/ui/tooltip'
import { MCPSidebar, MCPMain, SkillsSidebar, SkillsMain } from './components/library-views'
import { sourceIcon, sourceLabel } from './lib/source'
import type { Catalog, MCPCatalog, Message, Session, SessionDetail, SkillCatalog, SourceName, ToolCall } from './lib/types'
import { cn } from './lib/utils'

const sourceOrder: Array<'all' | SourceName> = ['all', 'opencode', 'codex', 'claude']
type ViewName = 'sessions' | 'skills' | 'mcp'
type SourceFilter = 'all' | SourceName
type PromptTarget = { id: string; nonce: number }

const viewCacheMs = 5_000

function viewFromURL(value: string | null): ViewName {
  return value === 'skills' || value === 'mcp' ? value : 'sessions'
}

export function App() {
  const [view, setView] = useState<ViewName>(() => viewFromURL(new URLSearchParams(location.search).get('view')))
  const [catalog, setCatalog] = useState<Catalog>({ sessions: [], sources: [] })
  const [activeID, setActiveID] = useState(() => new URLSearchParams(location.search).get('session') ?? '')
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [query, setQuery] = useState('')
  const [source, setSource] = useState<SourceFilter>('all')
  const [skillCatalog, setSkillCatalog] = useState<SkillCatalog>({ skills: [], sources: [] })
  const [activeSkillID, setActiveSkillID] = useState(() => new URLSearchParams(location.search).get('skill') ?? '')
  const [skillQuery, setSkillQuery] = useState('')
  const [skillSource, setSkillSource] = useState<SourceFilter>('all')
  const [mcpCatalog, setMCPCatalog] = useState<MCPCatalog>({ servers: [], sources: [] })
  const [activeMCPID, setActiveMCPID] = useState(() => new URLSearchParams(location.search).get('mcp') ?? '')
  const [mcpQuery, setMCPQuery] = useState('')
  const [mcpSource, setMCPSource] = useState<SourceFilter>('all')
  const [loadingCatalog, setLoadingCatalog] = useState(view === 'sessions')
  const [loadingSkills, setLoadingSkills] = useState(view === 'skills')
  const [loadingMCP, setLoadingMCP] = useState(view === 'mcp')
  const [loadingDetail, setLoadingDetail] = useState(false)
  const [refreshingDetail, setRefreshingDetail] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [mobilePickerOpen, setMobilePickerOpen] = useState(() => !new URLSearchParams(location.search).get('session'))
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return localStorage.getItem('closeview.sidebar.collapsed') === '1' } catch { return false }
  })
  const [outlineCollapsed, setOutlineCollapsed] = useState(() => {
    try { return localStorage.getItem('closeview.outline.collapsed') === '1' } catch { return false }
  })
  const [error, setError] = useState('')
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(() => new Set())
  const [detailRevision, setDetailRevision] = useState(0)
  const [promptTarget, setPromptTarget] = useState<PromptTarget | null>(null)
  const catalogFetchedAtRef = useRef(0)
  const skillsFetchedAtRef = useRef(0)
  const mcpFetchedAtRef = useRef(0)
  const detailFetchedAtRef = useRef(0)
  const detailIDRef = useRef('')

  const loadCatalog = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true)
    else setLoadingCatalog(true)
    try {
      const response = await fetch('/api/sessions', { cache: 'no-store' })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'Could not load sessions')
      setCatalog({ sessions: data.sessions ?? [], sources: data.sources ?? [] })
      catalogFetchedAtRef.current = Date.now()
      if (isRefresh) {
        detailFetchedAtRef.current = 0
        setDetailRevision(value => value + 1)
      }
      setError('')
      setActiveID(current => current || data.sessions?.[0]?.id || '')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not load sessions')
    } finally {
      setLoadingCatalog(false)
      setRefreshing(false)
    }
  }, [])

  const loadSkills = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true)
    else setLoadingSkills(true)
    try {
      const response = await fetch('/api/skills', { cache: 'no-store' })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'Could not load skills')
      const skills = data.skills ?? []
      setSkillCatalog({ skills, sources: data.sources ?? [] })
      skillsFetchedAtRef.current = Date.now()
      setActiveSkillID(current => skills.some((skill: { id: string }) => skill.id === current) ? current : skills[0]?.id || '')
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not load skills')
    } finally {
      setLoadingSkills(false)
      setRefreshing(false)
    }
  }, [])

  const loadMCP = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true)
    else setLoadingMCP(true)
    try {
      const response = await fetch('/api/mcp', { cache: 'no-store' })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'Could not load MCP servers')
      const servers = data.servers ?? []
      setMCPCatalog({ servers, sources: data.sources ?? [] })
      mcpFetchedAtRef.current = Date.now()
      setActiveMCPID(current => servers.some((server: { id: string }) => server.id === current) ? current : servers[0]?.id || '')
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not load MCP servers')
    } finally {
      setLoadingMCP(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    if (view !== 'sessions') return
    if (catalogFetchedAtRef.current && Date.now() - catalogFetchedAtRef.current < viewCacheMs) return
    void loadCatalog(catalogFetchedAtRef.current > 0)
  }, [loadCatalog, view])
  useEffect(() => {
    if (view !== 'skills') return
    if (skillsFetchedAtRef.current && Date.now() - skillsFetchedAtRef.current < viewCacheMs) return
    void loadSkills(skillsFetchedAtRef.current > 0)
  }, [loadSkills, view])
  useEffect(() => {
    if (view !== 'mcp') return
    if (mcpFetchedAtRef.current && Date.now() - mcpFetchedAtRef.current < viewCacheMs) return
    void loadMCP(mcpFetchedAtRef.current > 0)
  }, [loadMCP, view])

  useEffect(() => {
    try { localStorage.setItem('closeview.sidebar.collapsed', sidebarCollapsed ? '1' : '0') } catch { /* Storage is optional. */ }
  }, [sidebarCollapsed])

  useEffect(() => {
    try { localStorage.setItem('closeview.outline.collapsed', outlineCollapsed ? '1' : '0') } catch { /* Storage is optional. */ }
  }, [outlineCollapsed])

  useEffect(() => {
    if (view !== 'sessions' || !activeID) {
      setRefreshingDetail(false)
      setLoadingDetail(false)
      if (view === 'sessions' && !activeID) {
        setDetail(null)
        detailFetchedAtRef.current = 0
        detailIDRef.current = ''
      }
      return
    }

    const sameSession = detail?.session.id === activeID
    if (
      sameSession &&
      detailIDRef.current === activeID &&
      detailFetchedAtRef.current > 0 &&
      Date.now() - detailFetchedAtRef.current < viewCacheMs
    ) {
      setLoadingDetail(false)
      setRefreshingDetail(false)
      return
    }

    if (!sameSession) setDetail(null)
    const controller = new AbortController()
    setLoadingDetail(!sameSession)
    setRefreshingDetail(sameSession)
    fetch(`/api/sessions/${encodeURIComponent(activeID)}`, { cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const data = await response.json()
        if (!response.ok) throw new Error(data.error || 'Could not load session')
        return data as SessionDetail
      })
      .then(data => {
        setDetail(data)
        detailIDRef.current = activeID
        detailFetchedAtRef.current = Date.now()
        setError('')
        const url = new URL(location.href)
        url.searchParams.set('session', activeID)
        history.replaceState(null, '', url)
      })
      .catch(reason => {
        if (reason instanceof DOMException && reason.name === 'AbortError') return
        setError(reason instanceof Error ? reason.message : 'Could not load session')
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoadingDetail(false)
          setRefreshingDetail(false)
        }
      })
    return () => controller.abort()
  }, [activeID, detail?.session.id, detailRevision, view])

  const visibleSessionForest = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    const sessions = catalog.sessions.filter(session => {
      if (source !== 'all' && session.source !== source) return false
      return true
    })
    return filterSessionForest(buildSessionForest(sessions), normalized)
  }, [catalog.sessions, query, source])

  const sourceCounts = useMemo<Record<SourceName, number>>(() => ({
    opencode: catalog.sessions.filter(session => session.source === 'opencode').length,
    codex: catalog.sessions.filter(session => session.source === 'codex').length,
    claude: catalog.sessions.filter(session => session.source === 'claude').length,
  }), [catalog.sessions])
  const visibleSourceTabs = sourceOrder.filter(item => item === 'all' || sourceCounts[item] > 0)
  const skillSourceCounts = useMemo<Record<SourceName, number>>(() => ({
    opencode: skillCatalog.skills.filter(skill => skill.source === 'opencode').length,
    codex: skillCatalog.skills.filter(skill => skill.source === 'codex').length,
    claude: skillCatalog.skills.filter(skill => skill.source === 'claude').length,
  }), [skillCatalog.skills])
  const visibleSkillSourceTabs = sourceOrder.filter(item => item === 'all' || skillSourceCounts[item] > 0)
  const mcpSourceCounts = useMemo<Record<SourceName, number>>(() => ({
    opencode: mcpCatalog.servers.filter(server => server.source === 'opencode').length,
    codex: mcpCatalog.servers.filter(server => server.source === 'codex').length,
    claude: mcpCatalog.servers.filter(server => server.source === 'claude').length,
  }), [mcpCatalog.servers])
  const visibleMCPSourceTabs = sourceOrder.filter(item => item === 'all' || mcpSourceCounts[item] > 0)
  const visibleSkills = useMemo(() => {
    const term = skillQuery.trim().toLowerCase()
    return skillCatalog.skills.filter(skill => {
      if (skillSource !== 'all' && skill.source !== skillSource) return false
      if (!term) return true
      return [skill.name, skill.description, skill.path, skill.scope].join('\n').toLowerCase().includes(term)
    })
  }, [skillCatalog.skills, skillQuery, skillSource])
  const visibleMCPServers = useMemo(() => {
    const term = mcpQuery.trim().toLowerCase()
    return mcpCatalog.servers.filter(server => {
      if (mcpSource !== 'all' && server.source !== mcpSource) return false
      if (!term) return true
      return [server.name, server.transport, server.command ?? '', server.url ?? '', ...server.args, ...server.environment, ...server.headers].join('\n').toLowerCase().includes(term)
    })
  }, [mcpCatalog.servers, mcpQuery, mcpSource])
  const activeSources = view === 'skills' ? skillCatalog.sources : view === 'mcp' ? mcpCatalog.sources : catalog.sources
  const activeSourceErrors = activeSources.filter(source => !source.available && source.error)
  const activeSourceWarnings = activeSources.filter(source => source.warning)

  useEffect(() => {
    if (source !== 'all' && sourceCounts[source] === 0) setSource('all')
  }, [source, sourceCounts])
  useEffect(() => {
    if (skillSource !== 'all' && skillSourceCounts[skillSource] === 0) setSkillSource('all')
  }, [skillSource, skillSourceCounts])
  useEffect(() => {
    if (mcpSource !== 'all' && mcpSourceCounts[mcpSource] === 0) setMCPSource('all')
  }, [mcpSource, mcpSourceCounts])

  useEffect(() => {
    if (!activeID) return
    const parents = new Map(catalog.sessions.map(session => [session.id, session.parentId]))
    setExpandedSessions(current => {
      const next = new Set(current)
      let parentID = parents.get(activeID)
      while (parentID) {
        next.add(parentID)
        parentID = parents.get(parentID)
      }
      return next.size === current.size ? current : next
    })
  }, [activeID, catalog.sessions])

  async function deleteSession() {
    if (!detail) return
    setDeleting(true)
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(detail.session.id)}`, {
        method: 'DELETE',
        headers: { 'X-CloseView-Confirm': 'delete-session' },
      })
      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Could not delete session')
      }
      const deletedID = detail.session.id
      const next = detail.session.parentId || catalog.sessions.find(session => session.id !== deletedID)?.id || ''
      setCatalog(current => ({ ...current, sessions: current.sessions.filter(session => session.id !== deletedID) }))
      setDetail(null)
      setActiveID(next)
      if (!next) setMobilePickerOpen(true)
      setDeleteOpen(false)
      const url = new URL(location.href)
      if (next) url.searchParams.set('session', next)
      else url.searchParams.delete('session')
      history.replaceState(null, '', url)
      await loadCatalog(true)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not delete session')
    } finally {
      setDeleting(false)
    }
  }

  function selectView(next: ViewName) {
    setView(next)
    setMobilePickerOpen(false)
    const url = new URL(location.href)
    url.searchParams.set('view', next)
    url.searchParams.delete('session')
    url.searchParams.delete('skill')
    url.searchParams.delete('mcp')
    if (next === 'sessions' && activeID) url.searchParams.set('session', activeID)
    if (next === 'skills' && activeSkillID) url.searchParams.set('skill', activeSkillID)
    if (next === 'mcp' && activeMCPID) url.searchParams.set('mcp', activeMCPID)
    history.replaceState(null, '', url)
  }

  function selectSession(id: string) {
    setView('sessions')
    setActiveID(id)
    setPromptTarget(null)
    setMobilePickerOpen(false)
    const url = new URL(location.href)
    url.searchParams.set('view', 'sessions')
    url.searchParams.set('session', id)
    url.searchParams.delete('skill')
    url.searchParams.delete('mcp')
    history.replaceState(null, '', url)
  }

  function selectSkill(id: string) {
    setActiveSkillID(id)
    setMobilePickerOpen(false)
    const url = new URL(location.href)
    url.searchParams.set('view', 'skills')
    url.searchParams.set('skill', id)
    url.searchParams.delete('session')
    url.searchParams.delete('mcp')
    history.replaceState(null, '', url)
  }

  function selectMCP(id: string) {
    setActiveMCPID(id)
    setMobilePickerOpen(false)
    const url = new URL(location.href)
    url.searchParams.set('view', 'mcp')
    url.searchParams.set('mcp', id)
    url.searchParams.delete('session')
    url.searchParams.delete('skill')
    history.replaceState(null, '', url)
  }

  function refreshActiveView() {
    if (view === 'skills') void loadSkills(true)
    else if (view === 'mcp') void loadMCP(true)
    else void loadCatalog(true)
  }

  function toggleSidebar() {
    if (window.matchMedia('(max-width: 720px)').matches) {
      setMobilePickerOpen(false)
      return
    }
    setSidebarCollapsed(current => !current)
  }

  return (
    <div className={cn('app-shell', view !== 'sessions' && 'library-shell', sidebarCollapsed && 'sidebar-collapsed', outlineCollapsed && 'prompts-collapsed', mobilePickerOpen && 'mobile-picker-open')}>
      <aside id="sessions-panel" className={cn('session-sidebar', sidebarCollapsed && 'sidebar-collapsed', mobilePickerOpen ? 'mobile-open' : 'mobile-collapsed')}>
        <PanelResize side="left" />
        <div className="brand-row">
          <svg width={20} height={20} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="shrink-0">
            <circle cx={16} cy={16} r={11.5} />
            <path d="M13.5 14h5M13.5 18h3.5" />
          </svg>
          <div className="brand-name">CloseView</div>
          <Tooltip label={view === 'skills' ? 'Refresh skills' : view === 'mcp' ? 'Refresh MCP servers' : 'Refresh local sessions'}>
            <Button aria-label={view === 'skills' ? 'Refresh skills' : view === 'mcp' ? 'Refresh MCP servers' : 'Refresh local sessions'} size="icon" variant="ghost" className="ml-auto size-8" onClick={refreshActiveView} disabled={refreshing}>
              <IconRefresh size={16} className={cn(refreshing && 'animate-spin')} />
            </Button>
          </Tooltip>
          {(activeID || view !== 'sessions') && (
            <Button aria-label="Close navigation" size="icon" variant="ghost" className="mobile-picker-close size-8" onClick={() => setMobilePickerOpen(false)}>
              <IconX size={16} />
            </Button>
          )}
        </div>

        <div className="view-tabs" role="tablist" aria-label="CloseView view">
          {(['sessions', 'skills', 'mcp'] as const).map(item => (
            <button
              key={item}
              role="tab"
              aria-selected={view === item}
              className={cn('view-tab', view === item && 'active')}
              onClick={() => selectView(item)}
            >
              {item === 'sessions' ? <IconMessageCircle size={13} /> : item === 'skills' ? <IconSparkles size={13} /> : <IconTerminal2 size={13} />}
              {item === 'sessions' ? 'Sessions' : item === 'skills' ? 'Skills' : 'MCP'}
            </button>
          ))}
        </div>

        <div className="sidebar-controls">
          <div className="relative">
            <IconSearch className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" size={15} />
            <Input
              value={view === 'sessions' ? query : view === 'skills' ? skillQuery : mcpQuery}
              onChange={event => {
                const value = event.target.value
                if (view === 'sessions') setQuery(value)
                else if (view === 'skills') setSkillQuery(value)
                else setMCPQuery(value)
              }}
              placeholder={view === 'sessions' ? 'Search sessions' : view === 'skills' ? 'Search skills' : 'Search MCP servers'}
              className="pl-8"
            />
          </div>
          <div className="source-tabs" role="tablist" aria-label={view === 'sessions' ? 'Session source' : 'Library source'}>
            {(view === 'sessions' ? visibleSourceTabs : view === 'skills' ? visibleSkillSourceTabs : visibleMCPSourceTabs).map(item => {
              const active = view === 'sessions' ? source : view === 'skills' ? skillSource : mcpSource
              return (
                <button
                  key={item}
                  role="tab"
                  aria-selected={active === item}
                  className={cn('source-tab', active === item && 'active')}
                  onClick={() => {
                    if (view === 'sessions') setSource(item)
                    else if (view === 'skills') setSkillSource(item)
                    else setMCPSource(item)
                  }}
                >
                  {item === 'all' ? 'All' : sourceLabel(item)}
                </button>
              )
            })}
          </div>
        </div>

        <div className="session-list">
          {view === 'skills' ? (
            <SkillsSidebar
              catalog={{ ...skillCatalog, skills: visibleSkills }}
              activeID={activeSkillID}
              loading={loadingSkills}
              filtered={Boolean(skillQuery.trim()) || skillSource !== 'all'}
              onSelect={selectSkill}
            />
          ) : view === 'mcp' ? (
            <MCPSidebar
              catalog={{ ...mcpCatalog, servers: visibleMCPServers }}
              activeID={activeMCPID}
              loading={loadingMCP}
              filtered={Boolean(mcpQuery.trim()) || mcpSource !== 'all'}
              onSelect={selectMCP}
            />
          ) : loadingCatalog ? <SessionListSkeleton /> : visibleSessionForest.roots.length || visibleSessionForest.detached.length ? <>
            {visibleSessionForest.roots.map(node => (
              <SessionTree
                key={node.session.id}
                node={node}
                activeID={activeID}
                expanded={expandedSessions}
                forceExpanded={Boolean(query.trim())}
                onToggle={toggleExpanded(setExpandedSessions)}
                onSelect={selectSession}
              />
            ))}
            {visibleSessionForest.detached.length > 0 && (
              <DetachedSessions
                nodes={visibleSessionForest.detached}
                activeID={activeID}
                expanded={expandedSessions}
                forceExpanded={Boolean(query.trim())}
                onToggle={toggleExpanded(setExpandedSessions)}
                onSelect={selectSession}
              />
            )}
          </> : (
            <div className="sidebar-empty">
              <IconSearch size={22} />
              <span>{query ? 'No matching sessions' : 'No local sessions found'}</span>
            </div>
          )}
        </div>

        <SidebarToggle side="left" collapsed={sidebarCollapsed} onToggle={toggleSidebar} />
      </aside>

      <button
        aria-label="Close session navigation"
        className={cn('mobile-sidebar-backdrop', mobilePickerOpen && 'open')}
        onClick={() => setMobilePickerOpen(false)}
      />

      <main className="session-main">
        {(activeSourceErrors.length > 0 || activeSourceWarnings.length > 0 || error) && (
          <div className="error-stack" role="status" aria-live="polite">
            {activeSourceErrors.map(source => (
              <div key={source.name} className="error-banner"><IconAlertCircle size={16} /><span>{sourceLabel(source.name)} unavailable: {source.error}</span></div>
            ))}
            {activeSourceWarnings.map(source => (
              <div key={source.name} className="warning-banner"><IconAlertCircle size={16} /><span>{sourceLabel(source.name)}: {source.warning}</span></div>
            ))}
            {error && (
              <div className="error-banner"><IconAlertCircle size={16} /><span>{error}</span><button onClick={() => setError('')}>Dismiss</button></div>
            )}
          </div>
        )}
        {refreshingDetail && view === 'sessions' && <div className="detail-refresh-bar" aria-hidden="true" />}
        <div className={cn('session-workspace', view !== 'sessions' && 'is-hidden')}>
          {loadingDetail ? <DetailSkeleton /> : detail ? (
            <>
              <SessionHeader
                session={detail.session}
                parent={catalog.sessions.find(session => session.id === detail.session.parentId)}
                onOpenNav={() => setMobilePickerOpen(true)}
                onSelectParent={selectSession}
                onDelete={() => setDeleteOpen(true)}
              />
              <Transcript key={detail.session.id} detail={detail} promptTarget={promptTarget} />
            </>
          ) : (
            <div className="main-empty">
              <div className="empty-icon"><IconMessageCircle size={24} /></div>
              <h1>Select a session</h1>
              <p>Browse local OpenCode, Codex, and Claude history.</p>
            </div>
          )}
        </div>
        {view === 'skills' && <SkillsMain skillID={activeSkillID} onOpenNav={() => setMobilePickerOpen(true)} />}
        {view === 'mcp' && <MCPMain catalog={mcpCatalog} activeID={activeMCPID} onOpenNav={() => setMobilePickerOpen(true)} />}
      </main>

      {view === 'sessions' && (
        <aside id="prompts-panel" className={cn('outline-panel', outlineCollapsed && 'prompts-collapsed')}>
          <PanelResize side="right" />
          <div className="outline-title">Prompts</div>
          <div className="outline-list">
            {(detail?.messages ?? []).filter(message => message.role === 'user' && message.content.trim()).map(message => (
              <button
                key={message.id}
                title={message.content}
                onClick={() => setPromptTarget({ id: message.id, nonce: Date.now() })}
              >
                <strong>{firstLine(message.content)}</strong>
              </button>
            ))}
            {detail && !detail.messages.some(message => message.role === 'user') && <p>No user prompts.</p>}
          </div>
          <SidebarToggle side="right" collapsed={outlineCollapsed} onToggle={() => setOutlineCollapsed(current => !current)} />
        </aside>
      )}

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <div className="delete-icon"><IconTrash size={19} /></div>
          <AlertDialogTitle>Delete this {detail?.session.isSubsession ? 'sub-session' : `${detail ? sourceLabel(detail.session.source) : ''} session`}?</AlertDialogTitle>
          <AlertDialogDescription>
            This permanently removes <strong className="text-foreground">{detail?.session.title}</strong> from its native local session store. {(detail?.session.childCount ?? 0) > 0 && 'Child sessions may also be deleted. '}This cannot be undone by CloseView.
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={deleting} onClick={event => { event.preventDefault(); void deleteSession() }}>
              {deleting ? <IconRefresh size={15} className="animate-spin" /> : <IconTrash size={15} />}
              {deleting ? 'Deleting…' : 'Delete session'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface SessionNode {
  session: Session
  children: SessionNode[]
}

interface SessionForest {
  roots: SessionNode[]
  detached: SessionNode[]
}

const detachedGroupID = '__detached_subsessions__'

function DetachedSessions({ nodes, activeID, expanded, forceExpanded, onToggle, onSelect }: {
  nodes: SessionNode[]
  activeID: string
  expanded: Set<string>
  forceExpanded: boolean
  onToggle: (id: string) => void
  onSelect: (id: string) => void
}) {
  const isExpanded = forceExpanded || expanded.has(detachedGroupID) || nodes.some(node => sessionTreeContains(node, activeID))
  return (
    <div className="detached-sessions">
      <button className="detached-sessions-toggle" aria-expanded={isExpanded} onClick={() => onToggle(detachedGroupID)}>
        <IconChevronRight size={14} className={cn(isExpanded && 'expanded')} />
        <IconGitBranch size={14} />
        <span>Detached sub-sessions</span>
        <strong>{nodes.length}</strong>
      </button>
      {isExpanded && nodes.map(node => (
        <SessionTree
          key={node.session.id}
          node={node}
          activeID={activeID}
          expanded={expanded}
          forceExpanded={forceExpanded}
          onToggle={onToggle}
          onSelect={onSelect}
          depth={1}
        />
      ))}
    </div>
  )
}

function SessionTree({ node, activeID, expanded, forceExpanded, onToggle, onSelect, depth = 0 }: {
  node: SessionNode
  activeID: string
  expanded: Set<string>
  forceExpanded: boolean
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  depth?: number
}) {
  const hasChildren = node.children.length > 0
  const isExpanded = forceExpanded || expanded.has(node.session.id)
  return (
    <div className="session-tree">
      <div className="session-tree-row" style={{ '--tree-depth': depth } as React.CSSProperties}>
        <SessionRow
          session={node.session}
          active={node.session.id === activeID}
          onClick={() => onSelect(node.session.id)}
          leading={hasChildren ? (
            <button
              aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${node.session.title}`}
              aria-expanded={isExpanded}
              className="session-tree-toggle"
              onClick={event => { event.stopPropagation(); onToggle(node.session.id) }}
            >
              <IconChevronRight size={14} className={cn(isExpanded && 'expanded')} />
            </button>
          ) : undefined}
        />
      </div>
      {hasChildren && isExpanded && node.children.map(child => (
        <SessionTree
          key={child.session.id}
          node={child}
          activeID={activeID}
          expanded={expanded}
          forceExpanded={forceExpanded}
          onToggle={onToggle}
          onSelect={onSelect}
          depth={depth + 1}
        />
      ))}
    </div>
  )
}

function SessionRow({ session, active, onClick, leading }: { session: Session; active: boolean; onClick: () => void; leading?: React.ReactNode }) {
  return (
    <div
      className={cn('session-row', active && 'active')}
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onClick() } }}
    >
      {leading ?? <div className={cn('source-icon', `source-${session.source}`)}>{sourceIcon(session.source, 15)}</div>}
      <div className="min-w-0 flex-1">
        <div className="session-row-top"><strong>{session.title || 'Untitled session'}</strong><time>{relativeTime(session.updatedAt || session.createdAt)}</time></div>
        {session.childCount > 0 && (
          <div className="session-row-meta"><span>{session.childCount} sub-session{session.childCount === 1 ? '' : 's'}</span></div>
        )}
      </div>
    </div>
  )
}

function SessionHeader({ session, parent, onOpenNav, onSelectParent, onDelete }: {
  session: Session
  parent?: Session
  onOpenNav: () => void
  onSelectParent: (id: string) => void
  onDelete: () => void
}) {
  return (
    <header className="session-header">
      <Button aria-label="Open session navigation" variant="ghost" size="icon" className="mobile-session-menu" onClick={onOpenNav}>
        <IconMenu2 size={19} />
      </Button>
      <div className="session-header-info">
        <div className="header-title-row">
          <h1 title={session.title}>{session.title}</h1>
        </div>
        {(parent || session.model) && (
          <div className="header-meta">
            {parent && <button className="parent-session-link" onClick={() => onSelectParent(parent.id)}><IconGitBranch size={11} />{parent.title}</button>}
            {parent && session.model && <i />}
            {session.model && <span>{session.model}</span>}
          </div>
        )}
      </div>
      <div className="header-actions">
        {session.isSubsession && <Badge variant="outline" className="subsession-badge">Sub-session</Badge>}
        <Badge variant="outline" className={cn('source-badge', `source-${session.source}`)}>
          {sourceIcon(session.source, 12)}
          {sourceLabel(session.source)}
        </Badge>
        <Tooltip label="Delete session">
          <Button aria-label="Delete session" variant="ghost" size="icon" className="text-muted-foreground hover:bg-destructive/10 hover:text-red-300" onClick={onDelete}>
            <IconTrash size={17} />
          </Button>
        </Tooltip>
      </div>
    </header>
  )
}

function buildSessionForest(sessions: Session[]): SessionForest {
  const nodes = new Map(sessions.map(session => [session.id, { session, children: [] as SessionNode[] }]))
  const roots: SessionNode[] = []
  const detached: SessionNode[] = []
  for (const node of nodes.values()) {
    const parent = node.session.parentId ? nodes.get(node.session.parentId) : undefined
    if (parent && parent !== node) parent.children.push(node)
    else if (node.session.isSubsession) detached.push(node)
    else roots.push(node)
  }
  const sortNodes = (items: SessionNode[]) => {
    items.sort((left, right) => sessionTimestamp(right) - sessionTimestamp(left))
    items.forEach(item => sortNodes(item.children))
  }
  sortNodes(roots)
  sortNodes(detached)
  return { roots, detached }
}

function filterSessionForest(forest: SessionForest, query: string): SessionForest {
  if (!query) return forest
  return {
    roots: filterSessionTree(forest.roots, query),
    detached: filterSessionTree(forest.detached, query),
  }
}

function filterSessionTree(nodes: SessionNode[], query: string): SessionNode[] {
  if (!query) return nodes
  return nodes.flatMap(node => {
    const children = filterSessionTree(node.children, query)
    const matches = [node.session.title, node.session.threadId, node.session.projectPath, node.session.model, node.session.agent]
      .join('\n').toLowerCase().includes(query)
    return matches || children.length ? [{ ...node, children: matches ? node.children : children }] : []
  })
}

function toggleExpanded(setExpanded: React.Dispatch<React.SetStateAction<Set<string>>>) {
  return (id: string) => setExpanded(current => {
    const next = new Set(current)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
}

function sessionTimestamp(node: SessionNode): number {
  const own = new Date(node.session.updatedAt || node.session.createdAt).valueOf() || 0
  return node.children.reduce((latest, child) => Math.max(latest, sessionTimestamp(child)), own)
}

function sessionTreeContains(node: SessionNode, sessionID: string): boolean {
  return node.session.id === sessionID || node.children.some(child => sessionTreeContains(child, sessionID))
}

const transcriptWindowSize = 100
const transcriptStepSize = 60
type TranscriptRange = { start: number; end: number }

function latestTranscriptRange(count: number): TranscriptRange {
  return { start: Math.max(0, count - transcriptWindowSize), end: count }
}

function transcriptRangeFor(index: number, count: number): TranscriptRange {
  const start = Math.max(0, Math.min(index - Math.floor(transcriptWindowSize / 2), Math.max(0, count - transcriptWindowSize)))
  return normalizeTranscriptRange(start, start + transcriptWindowSize, count)
}

function normalizeTranscriptRange(start: number, end: number, count: number): TranscriptRange {
  if (count <= 0) return { start: 0, end: 0 }
  start = Math.max(0, Math.min(start, count - 1))
  end = Math.max(0, Math.min(end, count))
  if (end <= start) {
    if (start === 0) end = Math.min(transcriptWindowSize, count)
    else start = Math.max(0, end - transcriptWindowSize)
  }
  if (end - start > transcriptWindowSize) {
    if (start === 0) end = transcriptWindowSize
    else start = Math.max(0, end - transcriptWindowSize)
  }
  return { start, end }
}

function Transcript({ detail, promptTarget }: {
  detail: SessionDetail
  promptTarget: PromptTarget | null
}) {
  const [search, setSearch] = useState('')
  const deferredSearch = useDeferredValue(search)
  const [matchIndex, setMatchIndex] = useState(0)
  const [findOpen, setFindOpen] = useState(false)
  const [showLatest, setShowLatest] = useState(false)
  const [showAllOrphanTools, setShowAllOrphanTools] = useState(false)
  const [range, setRange] = useState<TranscriptRange>(() => latestTranscriptRange(detail.messages.length))
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const pendingScrollRef = useRef<'top' | 'bottom' | 'target' | null>(null)
  const pendingTargetIDRef = useRef('')
  const previousCountRef = useRef(detail.messages.length)
  const closeFind = useCallback(() => { setSearch(''); setMatchIndex(0); setFindOpen(false) }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target instanceof HTMLElement ? event.target : null
      const typing = target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA' || target?.isContentEditable
      if (((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'f') || (event.key === '/' && !typing)) {
        event.preventDefault()
        setFindOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  useEffect(() => { if (findOpen) inputRef.current?.focus() }, [findOpen])

  useEffect(() => {
    const count = detail.messages.length
    if (count === previousCountRef.current) return
    setRange(current => {
      const wasAtEnd = current.end === previousCountRef.current
      return wasAtEnd ? latestTranscriptRange(count) : normalizeTranscriptRange(current.start, current.end, count)
    })
    previousCountRef.current = count
  }, [detail.messages.length])

  const term = deferredSearch.trim().toLocaleLowerCase()
  const searchPending = term !== search.trim().toLocaleLowerCase()
  const matches = useMemo(() => term ? detail.messages.filter(message => message.content.toLocaleLowerCase().includes(term)) : [], [detail.messages, term])
  const safeMatchIndex = matches.length ? Math.min(matchIndex, matches.length - 1) : 0
  const activeMatch = matches[safeMatchIndex]?.id
  const visibleMessages = useMemo(() => detail.messages.slice(range.start, range.end), [detail.messages, range.end, range.start])

  useEffect(() => {
    const element = scrollRef.current
    if (element) setShowLatest(element.scrollHeight - element.scrollTop - element.clientHeight > 400)
  }, [detail.messages, range])

  const toolsByMessage = useMemo(() => {
    const grouped = new Map<string, ToolCall[]>()
    for (const tool of detail.toolCalls ?? []) grouped.set(tool.messageId, [...(grouped.get(tool.messageId) ?? []), tool])
    return grouped
  }, [detail.toolCalls])
  const allOrphanTools = useMemo(() => {
    const messageIDs = new Set(detail.messages.map(message => message.id))
    return (detail.toolCalls ?? []).filter(tool => !tool.messageId || !messageIDs.has(tool.messageId))
  }, [detail.messages, detail.toolCalls])
  const visibleOrphanTools = useMemo(
    () => showAllOrphanTools || allOrphanTools.length <= transcriptWindowSize ? allOrphanTools : allOrphanTools.slice(-transcriptWindowSize),
    [allOrphanTools, showAllOrphanTools],
  )
  const hiddenOrphanToolCount = useMemo(
    () => showAllOrphanTools ? 0 : Math.max(0, allOrphanTools.length - transcriptWindowSize),
    [allOrphanTools.length, showAllOrphanTools],
  )

  function showEarlier() {
    setRange(current => normalizeTranscriptRange(current.start - transcriptStepSize, current.end - transcriptStepSize, detail.messages.length))
    pendingScrollRef.current = 'top'
  }
  function showLater() {
    setRange(current => normalizeTranscriptRange(current.start + transcriptStepSize, current.end + transcriptStepSize, detail.messages.length))
    pendingScrollRef.current = 'bottom'
  }
  function showLatestMessages() {
    setRange(latestTranscriptRange(detail.messages.length))
    pendingScrollRef.current = 'bottom'
  }

  useEffect(() => {
    if (!promptTarget) return
    const index = detail.messages.findIndex(message => message.id === promptTarget.id)
    if (index < 0) return
    setRange(transcriptRangeFor(index, detail.messages.length))
    pendingScrollRef.current = 'target'
    pendingTargetIDRef.current = promptTarget.id
  }, [promptTarget])

  useEffect(() => {
    if (!activeMatch) return
    const index = detail.messages.findIndex(message => message.id === activeMatch)
    if (index < 0) return
    if (index < range.start || index >= range.end) setRange(transcriptRangeFor(index, detail.messages.length))
    pendingScrollRef.current = 'target'
    pendingTargetIDRef.current = activeMatch
  }, [activeMatch, detail.messages])

  useEffect(() => {
    const pending = pendingScrollRef.current
    if (!pending) return
    const element = scrollRef.current
    if (!element) return
    if (pending === 'top') {
      element.scrollTo({ top: 0, behavior: 'instant' })
      pendingScrollRef.current = null
      return
    }
    if (pending === 'bottom') {
      element.scrollTo({ top: element.scrollHeight, behavior: 'instant' })
      pendingScrollRef.current = null
      return
    }
    const target = document.getElementById(pendingTargetIDRef.current)
    if (!target) return
    if (target instanceof HTMLDetailsElement) target.open = true
    target.scrollIntoView({ block: 'center', behavior: 'instant' })
    pendingScrollRef.current = null
  }, [activeMatch, promptTarget, range])

  return (
    <div className="conversation">
      <div className="conversation-toolbar">
        {findOpen ? (
          <div className={cn('conversation-search', searchPending && 'search-pending')}>
            <IconSearch size={13} aria-hidden="true" />
            <input ref={inputRef} aria-label="Find in conversation" placeholder="Find…" value={search} onChange={event => { setSearch(event.target.value); setMatchIndex(0) }} onKeyDown={event => {
              if (event.key === 'Escape') closeFind()
              if (event.key === 'Enter' && matches.length) setMatchIndex((safeMatchIndex + (event.shiftKey ? matches.length - 1 : 1)) % matches.length)
            }} />
            {search && <><span aria-live="polite">{searchPending ? 'Searching…' : matches.length ? `${safeMatchIndex + 1}/${matches.length}` : 'No matches'}</span><button aria-label="Next match" disabled={!matches.length} onClick={() => setMatchIndex((safeMatchIndex + 1) % matches.length)}><IconChevronDown size={14} /></button></>}
            <button aria-label="Close search" onClick={closeFind}><IconX size={13} /></button>
          </div>
        ) : (
          <Tooltip label="Find in conversation ( / )">
            <button aria-label="Find in conversation" className="conversation-search-toggle" onClick={() => setFindOpen(true)}><IconSearch size={14} /></button>
          </Tooltip>
        )}
        {detail.messages.length > transcriptWindowSize && (
          <span className="conversation-range" aria-live="polite">
            Showing {formatNumber(range.start + 1)}–{formatNumber(range.end)} of {formatNumber(detail.messages.length)}
          </span>
        )}
      </div>
      <div className="transcript" id="transcript" ref={scrollRef} onScroll={event => {
        const element = event.currentTarget
        setShowLatest(element.scrollHeight - element.scrollTop - element.clientHeight > 400)
      }}>
        <div className="transcript-inner">
          {(detail.warnings ?? []).length > 0 && <div className="warning-card"><IconAlertCircle size={16} className="shrink-0" /><details className="min-w-0"><summary className="cursor-pointer">Some records in this session could not be fully parsed.</summary><ul className="mt-2 list-disc space-y-1 break-words pl-4">{detail.warnings.map((warning, index) => <li key={index}>{warning}</li>)}</ul></details></div>}
          {range.start > 0 && (
            <button className="transcript-pager" onClick={showEarlier}>
              Show {formatNumber(Math.min(transcriptStepSize, range.start))} earlier messages
            </button>
          )}
          {visibleMessages.map(message => <div key={`${message.sequence}-${message.id}`} className={cn(activeMatch === message.id && 'search-match')}><MessageCard message={message} source={detail.session.source} tools={toolsByMessage.get(message.id) ?? []} /></div>)}
          {range.end < detail.messages.length && (
            <button className="transcript-pager" onClick={showLater}>
              Show {formatNumber(Math.min(transcriptStepSize, detail.messages.length - range.end))} later messages
            </button>
          )}
          {hiddenOrphanToolCount > 0 && (
            <button className="transcript-pager" onClick={() => setShowAllOrphanTools(true)}>
              Show {formatNumber(hiddenOrphanToolCount)} earlier detached tool records
            </button>
          )}
          {visibleOrphanTools.map(tool => <ToolCard key={tool.id} tool={tool} />)}
          {!detail.messages.length && !allOrphanTools.length && <div className="transcript-empty">This session has no viewable messages.</div>}
        </div>
      </div>
      {showLatest && <button className="jump-latest" onClick={() => { setSearch(''); showLatestMessages() }}><IconChevronDown size={15} />Latest</button>}
    </div>
  )
}


function MessageCard({ message, source, tools }: { message: Message; source: SourceName; tools: ToolCall[] }) {
  const [copied, setCopied] = useState(false)
  const [collapsibleOpen, setCollapsibleOpen] = useState(false)
  const isReasoning = message.role === 'reasoning'
  if (isReasoning) {
    return (
      <details id={message.id} className="reasoning-card" open={collapsibleOpen} onToggle={event => setCollapsibleOpen(event.currentTarget.open)}>
        <summary><IconSparkles size={15} /><span>Reasoning summary</span><IconChevronDown size={15} className="ml-auto chevron" /></summary>
        {collapsibleOpen && <div className="reasoning-content"><RichText text={message.content} /></div>}
      </details>
    )
  }
  if (message.role === 'system') {
    return (
      <details id={message.id} className="context-card" open={collapsibleOpen} onToggle={event => setCollapsibleOpen(event.currentTarget.open)}>
        <summary><IconCode size={15} /><span>Session context</span><time>{formatTime(message.createdAt)}</time><IconChevronDown size={15} className="ml-auto chevron" /></summary>
        {collapsibleOpen && <div className="context-content"><RichText text={message.content} /></div>}
      </details>
    )
  }
  return (
    <article id={message.id} className={cn('message-card', `role-${message.role}`)}>
      <div className="message-body">
        <div className="message-heading">
          <strong>{message.role === 'assistant' ? sourceLabel(source) : roleLabel(message.role)}</strong>
          {message.createdAt && <time>{formatTime(message.createdAt)}</time>}
          <Tooltip label={copied ? 'Copied' : 'Copy message'}>
            <Button aria-label="Copy message" variant="ghost" size="icon" className="ml-auto size-6 text-muted-foreground" onClick={async () => {
              await navigator.clipboard.writeText(message.content)
              setCopied(true)
              setTimeout(() => setCopied(false), 1000)
            }}>{copied ? <IconCheck size={14} /> : <IconCopy size={14} />}</Button>
          </Tooltip>
        </div>
        <div className={cn('message-content', (message.role === 'user' || message.role === 'assistant') && 'markdown-content')}>
          {message.role === 'user' || message.role === 'assistant' ? <MarkdownContent text={message.content} /> : <RichText text={message.content} />}
        </div>
        {message.role === 'assistant' && message.model && <div className="message-model">{message.model}</div>}
        {tools.map(tool => <ToolCard key={tool.id} tool={tool} />)}
      </div>
    </article>
  )
}

function ToolCard({ tool }: { tool: ToolCall }) {
  const [open, setOpen] = useState(false)
  return (
    <details className="tool-card" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
      <summary>
        <div className="tool-icon">{tool.kind === 'shell' ? <IconTerminal2 size={14} /> : <IconCode size={14} />}</div>
        <strong>{tool.name || 'Tool'}</strong>
        <span>{tool.status || 'unknown'}</span>
        <IconChevronDown size={14} className="ml-auto chevron" />
      </summary>
      {open && (
        <div className="tool-content">
          {tool.input && <ToolSection label="Input" value={tool.input} />}
          {tool.output && <ToolSection label="Output" value={tool.output} />}
        </div>
      )}
    </details>
  )
}

function ToolSection({ label, value }: { label: string; value: string }) {
  return <section><div>{label}</div><pre><code>{value}</code></pre></section>
}

function RichText({ text }: { text: string }) {
  const chunks = text.split(/```([^\n`]*)\n([\s\S]*?)```/g)
  return <>{chunks.map((chunk, index) => index % 3 === 2 ? <pre key={index}><code>{chunk.replace(/\n$/, '')}</code></pre> : index % 3 === 1 ? null : <p key={index}>{chunk}</p>)}</>
}

function SidebarToggle({ side, collapsed, onToggle }: { side: 'left' | 'right'; collapsed: boolean; onToggle: () => void }) {
  const label = `${collapsed ? 'Expand' : 'Collapse'} ${side} sidebar`
  const Icon = side === 'left'
    ? (collapsed ? IconLayoutSidebarLeftExpand : IconLayoutSidebarLeftCollapse)
    : (collapsed ? IconLayoutSidebarRightExpand : IconLayoutSidebarRightCollapse)
  return (
    <div className="sidebar-footer">
      <button
        type="button"
        className="sidebar-collapse-toggle"
        aria-label={label}
        title={label}
        aria-expanded={!collapsed}
        aria-controls={side === 'left' ? 'sessions-panel' : 'prompts-panel'}
        onClick={onToggle}
      >
        <Icon size={18} stroke={1.75} aria-hidden="true" />
      </button>
    </div>
  )
}

function SessionListSkeleton() {
  return <div className="space-y-2 p-2">{Array.from({ length: 7 }).map((_, index) => <div className="flex gap-3 p-2" key={index}><Skeleton className="size-8" /><div className="flex-1 space-y-2"><Skeleton className="h-3 w-4/5" /><Skeleton className="h-2.5 w-2/5" /></div></div>)}</div>
}

function DetailSkeleton() {
  return <div className="p-6"><div className="mb-10 flex gap-3"><Skeleton className="size-9" /><div className="space-y-2"><Skeleton className="h-5 w-64" /><Skeleton className="h-3 w-96" /></div></div><div className="mx-auto max-w-3xl space-y-6">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className={cn('h-28', index % 2 && 'ml-20')} />)}</div></div>
}

function roleLabel(role: string) { return role === 'user' ? 'You' : role === 'assistant' ? 'Assistant' : role === 'system' ? 'Context' : role === 'tool' ? 'Tool' : role || 'Message' }
function firstLine(value: string) { return value.trim().split('\n').find(Boolean)?.slice(0, 90) || 'Prompt' }
function formatNumber(value: number) { return new Intl.NumberFormat().format(value) }
function formatDate(value: string) { if (!value) return 'Unknown date'; const date = new Date(value); return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date) }
function formatTime(value: string) { if (!value) return ''; const date = new Date(value); return Number.isNaN(date.valueOf()) ? '' : new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(date) }
function relativeTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return ''
  const delta = date.valueOf() - Date.now()
  const minutes = Math.round(delta / 60000)
  if (Math.abs(minutes) < 60) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(hours, 'hour')
  const days = Math.round(hours / 24)
  if (Math.abs(days) < 30) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(days, 'day')
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(date)
}
