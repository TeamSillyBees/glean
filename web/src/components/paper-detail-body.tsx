import { useEffect } from 'react'
import {
  type Query,
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { ArrowUpRight, Check, EyeOff } from 'lucide-react'
import type { Paper, PaperPage, ReadStatus } from '../lib/types'
import { api, ApiError } from '../lib/api'
import { readLabels, safeLink } from '../lib/utils'
import { Badge, ErrorState, ExternalLink, Loading } from './common'
import { Button } from './ui/button'
import { DialogClose } from './ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Tooltip } from './ui/tooltip'
import { PaperOverview, PaperEvidence } from './paper-content'
import { CatalogManagement } from './catalog-management'
import type { PaperBodyProps } from './deferred-paper-body'

export default function PaperDetailBody({
  id,
  notify,
  canManage = false,
  topics = [],
  activeTopic,
}: PaperBodyProps) {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['paper', id],
    queryFn: ({ signal }) => api.paper(id!, signal),
    enabled: !!id,
    staleTime: 0,
  })
  const pending = useIsMutating({ mutationKey: ['paper-state', id] }) > 0
  const mutation = useMutation({
    mutationKey: ['paper-state', id],
    onMutate: async ({
      paper,
      originalQuery,
    }: {
      paper: Paper
      hidden?: boolean
      readStatus?: ReadStatus
      originalQuery: Query
    }) => {
      if (
        client.getQueryCache().find({ queryKey: ['paper', paper.id], exact: true }) !==
        originalQuery
      ) {
        throw new Error('会话已切换，已取消旧操作。')
      }
      // 旧 GET 不能在写入完成后覆盖个人状态。
      await Promise.all([
        client.cancelQueries({ queryKey: ['paper', paper.id] }),
        client.cancelQueries({ queryKey: ['papers'] }),
      ])
      return { originalQuery }
    },
    mutationFn: ({ paper, hidden, readStatus, originalQuery }) => {
      if (
        client.getQueryCache().find({ queryKey: ['paper', paper.id], exact: true }) !==
        originalQuery
      ) {
        throw new Error('会话已切换，已取消旧操作。')
      }
      // 使用上一笔写入返回的权威缓存，不使用按钮闭包中的旧字段。
      const latest = client.getQueryData<Paper>(['paper', paper.id]) || paper
      return api.state(paper.id, hidden ?? latest.hidden, readStatus ?? latest.readStatus)
    },
    onSuccess: async (state, variables, context) => {
      const key = ['paper', variables.paper.id]
      const ownsCache = () =>
        context?.originalQuery &&
        client.getQueryCache().find({ queryKey: key, exact: true }) === context.originalQuery
      if (!ownsCache()) return
      // 写入期间关闭、重开详情也可能发起 GET，合并前再次取消。
      await Promise.all([
        client.cancelQueries({ queryKey: key }),
        client.cancelQueries({ queryKey: ['papers'] }),
      ])
      // 登出会清空 QueryClient；旧会话的完成回调不得重建或污染新会话缓存。
      if (!ownsCache()) return
      client.setQueryData<Paper>(key, (cached) => ({ ...variables.paper, ...cached, ...state }))
      client.setQueriesData<PaperPage>({ queryKey: ['papers'] }, (page) =>
        page
          ? {
              ...page,
              items: page.items.map((item) =>
                item.id === variables.paper.id ? { ...item, ...state } : item,
              ),
            }
          : page,
      )
      void client.invalidateQueries({ queryKey: ['papers'] })
      void client.invalidateQueries({ queryKey: ['facets'] })
      notify(
        variables.hidden === true
          ? '已在所有主题中隐藏，可在「已隐藏」中恢复。'
          : variables.hidden === false
            ? '已恢复显示。'
            : '阅读状态已保存。',
      )
    },
  })
  function changeState(paper: Paper, change: { hidden?: boolean; readStatus?: ReadStatus }) {
    // QueryClient 的 mutation 状态同步更新，阻止同一帧重复点击及重开后的并发写入。
    if (client.isMutating({ mutationKey: ['paper-state', paper.id] })) return
    const originalQuery = client
      .getQueryCache()
      .find({ queryKey: ['paper', paper.id], exact: true })
    if (!originalQuery) return
    mutation.mutate({ paper, ...change, originalQuery })
  }
  const { reset: resetMutation } = mutation
  useEffect(() => resetMutation(), [id, resetMutation])
  const paper = query.data
  return (
    <>
      {query.isLoading ? (
        <Loading />
      ) : query.isError ? (
        <div className="p-7">
          {query.error instanceof ApiError && query.error.status === 404 ? (
            <div role="alert">
              <h2 className="font-semibold">未找到这篇论文</h2>
              <p className="mt-2 text-sm text-muted-foreground">
                论文可能不存在。请返回列表，或检查链接。
              </p>
              <DialogClose asChild>
                <Button variant="outline" className="mt-3">
                  返回列表
                </Button>
              </DialogClose>
            </div>
          ) : (
            <ErrorState error={query.error} retry={() => query.refetch()} />
          )}
        </div>
      ) : (
        paper && (
          <div className="px-5 py-6 sm:px-8">
            <div className="mb-4 flex flex-wrap items-center gap-2">
              <Badge>{paper.venue || '来源待核验'}</Badge>
              {paper.year > 0 && <Badge>{paper.year}</Badge>}
              {paper.review?.isMock && <Badge tone="violet">模型总结为模拟输出</Badge>}
              {paper.hidden && <Badge tone="amber">已隐藏</Badge>}
            </div>
            <h2 className="break-words text-xl font-semibold leading-[1.6] text-foreground sm:text-2xl [overflow-wrap:anywhere]">
              {paper.titleZh || paper.title}
            </h2>
            {paper.titleZh && (
              <p className="mt-3 break-words text-sm leading-7 text-muted-foreground [overflow-wrap:anywhere]">
                {paper.title}
              </p>
            )}
            <p className="mt-4 break-words text-xs leading-6 text-muted-foreground">
              {paper.authors?.length ? paper.authors.join(' · ') : '作者信息待补充'}
            </p>
            <div className="mt-5 flex flex-wrap items-center gap-4">
              {safeLink(paper.url) && (
                <Button asChild>
                  <a href={safeLink(paper.url)} target="_blank" rel="noreferrer noopener">
                    打开论文原文
                    <ArrowUpRight />
                  </a>
                </Button>
              )}
              {paper.doi && (
                <ExternalLink href={`https://doi.org/${encodeURIComponent(paper.doi)}`}>
                  DOI
                </ExternalLink>
              )}
              {paper.arxivId && (
                <ExternalLink href={`https://arxiv.org/abs/${encodeURIComponent(paper.arxivId)}`}>
                  arXiv
                </ExternalLink>
              )}
            </div>
            <div
              role="group"
              aria-label="个人阅读与隐藏"
              className="mt-6 flex flex-wrap items-center gap-2 border-t pt-5"
            >
              {(['unread', 'reading', 'read'] as ReadStatus[]).map((status) => (
                <Button
                  key={status}
                  aria-pressed={paper.readStatus === status}
                  size="sm"
                  variant={paper.readStatus === status ? 'default' : 'outline'}
                  disabled={pending}
                  onClick={() => changeState(paper, { readStatus: status })}
                >
                  {paper.readStatus === status && <Check />}
                  {readLabels[status]}
                </Button>
              ))}
              <Tooltip
                label={paper.hidden ? '在所有主题中恢复显示' : '在所有主题中隐藏，可随时恢复'}
              >
                <Button
                  className="ml-auto"
                  variant="ghost"
                  size="sm"
                  disabled={pending}
                  onClick={() => changeState(paper, { hidden: !paper.hidden })}
                >
                  <EyeOff />
                  {paper.hidden ? '恢复显示' : '隐藏'}
                </Button>
              </Tooltip>
            </div>
            <p className="mt-3 text-xs text-muted-foreground">
              隐藏状态适用于所有主题；可在「已隐藏」中恢复。个人阅读状态不会改变原始审读记录。
            </p>
            {mutation.isError && mutation.variables?.paper.id === id && (
              <p role="alert" className="mt-3 text-sm text-red-700">
                {mutation.error.message}
              </p>
            )}
            <Tabs defaultValue="overview" className="mt-8">
              <TabsList className="w-full">
                <TabsTrigger className="flex-1" value="overview">
                  内容概览
                </TabsTrigger>
                <TabsTrigger className="flex-1" value="evidence">
                  来源与证据
                </TabsTrigger>
              </TabsList>
              <TabsContent value="overview">
                <PaperOverview
                  paper={paper}
                  canManage={canManage}
                  topicNames={Object.fromEntries(topics.map((topic) => [topic.slug, topic.name]))}
                />
              </TabsContent>
              <TabsContent value="evidence">
                <PaperEvidence
                  paper={paper}
                  topicNames={Object.fromEntries(topics.map((topic) => [topic.slug, topic.name]))}
                />
              </TabsContent>
            </Tabs>
            {canManage && (
              <CatalogManagement
                key={paper.id}
                paper={paper}
                topics={topics}
                activeTopic={activeTopic}
                notify={notify}
              />
            )}
          </div>
        )
      )}
    </>
  )
}
