import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { SaveConnections, LoadConnections } from '../../bindings/github.com/ys-ll/uniterm/app'
import { Events } from '@wailsio/runtime'
import type { ConnectionConfig, ConnectionGroup, ConnectionColor, ConnectionIcon } from '../types/session'

// Module-level un-subscriber for the cross-window store:connections:changed listener.
// Tracked at module scope so re-imports under HMR can detach the previous
// listener before re-subscribing (FE-03).
let unsubConnectionsChanged: (() => void) | null = null

export interface GroupTreeNode {
  group: ConnectionGroup
  connections: ConnectionConfig[]
  children: GroupTreeNode[]
}

export interface GroupedConnections {
  roots: GroupTreeNode[]
  ungrouped: ConnectionConfig[]
}

// One-time migration of legacy configs: Redis/MongoDB/Elasticsearch used to be
// stored as type "database" with a dbType discriminator; they are standalone
// types now. Idempotent — already-migrated configs pass through untouched.
// Applied on load and on add so imports/synced stores are covered too.
export function migrateLegacyDatabaseTypes(connections: ConnectionConfig[]): ConnectionConfig[] {
  for (const c of connections) {
    if (c.type === 'database' && (c.dbType === 'redis' || c.dbType === 'mongodb' || c.dbType === 'elasticsearch')) {
      c.type = c.dbType
      delete c.dbType
    }
  }
  return connections
}

// ── Deferred deletion (undo window) ──
// A deletion leaves the screen at once but only reaches the disk (and with it
// the vault/sync) when its undo window runs out. Until then save() writes the
// pending items back in place, so any other save in the meantime, a crash or
// quitting the app keeps them: an unfinished deletion never happens. Each
// deletion has its own deadline; undoing one restores it where it was.
export const DELETE_UNDO_MS = 8000

interface Placed<T> { item: T; index: number }
interface FieldPatch { id: string; from: string | undefined; to: string | undefined }

interface DeleteOp {
  conns: Placed<ConnectionConfig>[]
  groups: Placed<ConnectionGroup>[]
  connPatches: FieldPatch[]   // groupId changes (connections moved out)
  groupPatches: FieldPatch[]  // parentId changes (child groups moved up)
  timer: ReturnType<typeof setTimeout>
  commit: () => Promise<void>
}

export interface PendingDelete {
  id: string
  kind: 'connections' | 'group'
  names: string[]       // removed connections, in list order
  host?: string         // set when exactly one connection goes
  groupName?: string
}

// Put a deletion back into the given arrays: patched fields first (only when
// nothing changed them since), then the removed items at their old index.
function revertDelete(op: Pick<DeleteOp, 'conns' | 'groups' | 'connPatches' | 'groupPatches'>, conns: ConnectionConfig[], groups: ConnectionGroup[]) {
  for (const p of op.connPatches) {
    const c = conns.find(x => x.id === p.id)
    if (c && c.groupId === p.to) c.groupId = p.from
  }
  for (const p of op.groupPatches) {
    const g = groups.find(x => x.id === p.id)
    if (g && g.parentId === p.to) g.parentId = p.from
  }
  for (const { item, index } of [...op.groups].sort((a, b) => a.index - b.index)) {
    if (!groups.some(g => g.id === item.id)) groups.splice(Math.min(index, groups.length), 0, item)
  }
  for (const { item, index } of [...op.conns].sort((a, b) => a.index - b.index)) {
    if (!conns.some(c => c.id === item.id)) conns.splice(Math.min(index, conns.length), 0, item)
  }
}

// Apply a pending deletion to state that still has it (another window's
// broadcast, read from the disk). Keeps the newest copy of each removed item
// so an undo brings back what the other window last wrote.
function applyDelete(op: DeleteOp, conns: ConnectionConfig[], groups: ConnectionGroup[]): { conns: ConnectionConfig[]; groups: ConnectionGroup[] } {
  const connIds = new Set(op.conns.map(p => p.item.id))
  const groupIds = new Set(op.groups.map(p => p.item.id))
  op.conns.forEach((p) => {
    const i = conns.findIndex(c => c.id === p.item.id)
    if (i >= 0) { p.item = conns[i]; p.index = i }
  })
  op.groups.forEach((p) => {
    const i = groups.findIndex(g => g.id === p.item.id)
    if (i >= 0) { p.item = groups[i]; p.index = i }
  })
  conns = conns.filter(c => !connIds.has(c.id))
  groups = groups.filter(g => !groupIds.has(g.id))
  for (const p of op.connPatches) {
    const c = conns.find(x => x.id === p.id)
    if (c && c.groupId === p.from) c.groupId = p.to
  }
  for (const p of op.groupPatches) {
    const g = groups.find(x => x.id === p.id)
    if (g && g.parentId === p.from) g.parentId = p.to
  }
  return { conns, groups }
}

export const useConnectionStore = defineStore('connection', () => {
  const connections = ref<ConnectionConfig[]>([])
  const groups = ref<ConnectionGroup[]>([])
  const loading = ref(false)
  // Insertion-ordered: the oldest deletion is reverted last when persisting.
  const deleteOps = new Map<string, DeleteOp>()
  const pendingDeletes = ref<PendingDelete[]>([])

  async function load() {
    loading.value = true
    try {
      const data = await LoadConnections() as { groups?: ConnectionGroup[]; connections?: ConnectionConfig[] }
      // The disk still has the pending deletions: keep them off the screen.
      let conns = migrateLegacyDatabaseTypes((data.connections || []) as ConnectionConfig[])
      let grps = data.groups || []
      for (const op of deleteOps.values()) ({ conns, groups: grps } = applyDelete(op, conns, grps))
      groups.value = grps
      connections.value = conns
    } catch (e) {
      console.error('Failed to load connections:', e)
    } finally {
      loading.value = false
    }
  }

  // What goes to the disk: the live state with every pending deletion put
  // back. Copies, so the reverted fields never touch what is on screen.
  function persistedState() {
    const conns = connections.value.map(c => ({ ...c }))
    const grps = groups.value.map(g => ({ ...g }))
    for (const op of [...deleteOps.values()].reverse()) {
      revertDelete({
        ...op,
        conns: op.conns.map(p => ({ item: { ...p.item }, index: p.index })),
        groups: op.groups.map(p => ({ item: { ...p.item }, index: p.index })),
      }, conns, grps)
    }
    return { groups: grps, connections: conns }
  }

  async function save() {
    try {
      await SaveConnections(persistedState() as any)
    } catch (e) {
      console.error('Failed to save connections:', e)
    }
  }

  async function add(config: ConnectionConfig) {
    migrateLegacyDatabaseTypes([config])
    if (!config.id) {
      config.id = `conn-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    }
    if (connections.value.some(c => c.id === config.id)) {
      return
    }
    connections.value.push(config)
    await save()
  }

  async function update(id: string, config: Partial<ConnectionConfig>) {
    const idx = connections.value.findIndex(c => c.id === id)
    if (idx >= 0) {
      connections.value[idx] = { ...connections.value[idx], ...config }
      await save()
    }
  }

  async function remove(id: string) {
    connections.value = connections.value.filter(c => c.id !== id)
    await save()
  }

  async function removeMany(ids: string[]) {
    const set = new Set(ids)
    connections.value = connections.value.filter(c => !set.has(c.id))
    await save()
  }

  // ── Group CRUD ──

  function generateGroupId(): string {
    return `grp-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
  }

  function uniqueGroupName(baseName: string, parentId: string | undefined, excludeId?: string): string {
    const siblings = groups.value.filter(g => g.parentId === parentId && g.id !== excludeId)
    if (!siblings.some(g => g.name === baseName)) return baseName
    let n = 1
    while (siblings.some(g => g.name === `${baseName} (${n})`)) n++
    return `${baseName} (${n})`
  }

  async function addGroup(name: string, parentId?: string): Promise<ConnectionGroup> {
    const uniqueName = uniqueGroupName(name, parentId)
    const group: ConnectionGroup = { id: generateGroupId(), name: uniqueName, parentId }
    groups.value.push(group)
    await save()
    return group
  }

  // '' clears the field (inherit from the parent group).
  async function setGroupStyle(id: string, color: ConnectionColor | '', icon: ConnectionIcon | '') {
    const g = groups.value.find(g => g.id === id)
    if (!g) return
    g.color = color || undefined
    g.icon = icon || undefined
    await save()
  }

  async function renameGroup(id: string, name: string) {
    const g = groups.value.find(g => g.id === id)
    if (g) {
      g.name = uniqueGroupName(name, g.parentId, id)
      await save()
    }
  }

  type GroupConnAction = 'delete-connections' | 'move-out'
  type GroupChildAction = 'move-up' | 'delete-all'

  // In-memory part of deleteGroup; the caller decides when to save.
  function applyDeleteGroup(id: string, connAction: GroupConnAction, childAction: GroupChildAction) {
    const targetGroup = groups.value.find(g => g.id === id)
    const parentId = targetGroup?.parentId

    // Handle child groups
    if (childAction === 'move-up') {
      for (const g of groups.value) {
        if (g.parentId === id) {
          g.parentId = parentId
        }
      }
    } else {
      // Cascade delete: collect all descendant group IDs
      const toDelete = new Set<string>()
      function collectDescendants(gid: string) {
        for (const g of groups.value) {
          if (g.parentId === gid) {
            toDelete.add(g.id)
            collectDescendants(g.id)
          }
        }
      }
      toDelete.add(id)
      collectDescendants(id)
      connections.value = connections.value.filter(c => {
        const gid = c.groupId
        return !gid || !toDelete.has(gid)
      })
      groups.value = groups.value.filter(g => !toDelete.has(g.id))
      return
    }

    // Handle connections
    if (connAction === 'delete-connections') {
      connections.value = connections.value.filter(c => c.groupId !== id)
    } else {
      for (const c of connections.value) {
        if (c.groupId === id) {
          c.groupId = undefined
        }
      }
    }
    groups.value = groups.value.filter(g => g.id !== id)
  }

  async function deleteGroup(id: string, connAction: GroupConnAction, childAction: GroupChildAction = 'move-up') {
    applyDeleteGroup(id, connAction, childAction)
    await save()
  }

  // Runs mutate on the live state and records what it took away (and which
  // groupId/parentId it changed) as a pending deletion with its own timer.
  function scheduleDelete(kind: PendingDelete['kind'], mutate: () => void, commit: () => Promise<void>, groupName?: string): string | null {
    const connIndex = new Map(connections.value.map((c, i) => [c.id, i]))
    const groupIndex = new Map(groups.value.map((g, i) => [g.id, i]))
    const beforeConns = connections.value.slice()
    const beforeGroups = groups.value.slice()
    const connGroup = new Map(beforeConns.map(c => [c.id, c.groupId]))
    const groupParent = new Map(beforeGroups.map(g => [g.id, g.parentId]))
    mutate()
    const keptConns = new Set(connections.value.map(c => c.id))
    const keptGroups = new Set(groups.value.map(g => g.id))
    const conns = beforeConns.filter(c => !keptConns.has(c.id)).map(c => ({ item: c, index: connIndex.get(c.id)! }))
    const grps = beforeGroups.filter(g => !keptGroups.has(g.id)).map(g => ({ item: g, index: groupIndex.get(g.id)! }))
    const connPatches = connections.value.filter(c => connGroup.get(c.id) !== c.groupId)
      .map(c => ({ id: c.id, from: connGroup.get(c.id), to: c.groupId }))
    const groupPatches = groups.value.filter(g => groupParent.get(g.id) !== g.parentId)
      .map(g => ({ id: g.id, from: groupParent.get(g.id), to: g.parentId }))
    if (!conns.length && !grps.length) return null

    const opId = `del-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
    const timer = setTimeout(() => { void commitDelete(opId) }, DELETE_UNDO_MS)
    deleteOps.set(opId, { conns, groups: grps, connPatches, groupPatches, timer, commit })
    pendingDeletes.value = [...pendingDeletes.value, {
      id: opId,
      kind,
      names: conns.map(p => p.item.name || p.item.host),
      host: conns.length === 1 ? conns[0].item.host : undefined,
      groupName,
    }]
    return opId
  }

  // Deferred removeMany: the connections leave the screen now, the disk in
  // DELETE_UNDO_MS unless undoDelete runs first. Returns the undo handle.
  function scheduleRemoveMany(ids: string[]): string | null {
    const set = new Set(ids)
    return scheduleDelete('connections',
      () => { connections.value = connections.value.filter(c => !set.has(c.id)) },
      () => removeMany(ids))
  }

  function scheduleDeleteGroup(id: string, connAction: GroupConnAction, childAction: GroupChildAction = 'move-up'): string | null {
    const name = groups.value.find(g => g.id === id)?.name
    return scheduleDelete('group', () => applyDeleteGroup(id, connAction, childAction), () => save(), name)
  }

  async function commitDelete(opId: string) {
    const op = deleteOps.get(opId)
    if (!op) return
    clearTimeout(op.timer)
    deleteOps.delete(opId)
    pendingDeletes.value = pendingDeletes.value.filter(p => p.id !== opId)
    // Re-apply on the live state first: a load() or broadcast in between may
    // have swapped it for the disk copy, and the commit saves what is live.
    const live = applyDelete(op, connections.value, groups.value)
    connections.value = live.conns
    groups.value = live.groups
    await op.commit()
  }

  // Puts the deletion back as it was. The disk never lost it, so no save.
  function undoDelete(opId: string): boolean {
    const op = deleteOps.get(opId)
    if (!op) return false
    clearTimeout(op.timer)
    deleteOps.delete(opId)
    pendingDeletes.value = pendingDeletes.value.filter(p => p.id !== opId)
    const conns = connections.value.slice()
    const grps = groups.value.slice()
    revertDelete(op, conns, grps)
    connections.value = conns
    groups.value = grps
    return true
  }


  async function reparentGroup(groupId: string, newParentId: string | undefined) {
    // Prevent self-parenting and cycles
    if (newParentId === groupId) return
    if (newParentId && isDescendantOf(groupId, newParentId)) return

    const g = groups.value.find(g => g.id === groupId)
    if (!g) return

    // Compute new name BEFORE changing parent (check against new siblings)
    const newName = uniqueGroupName(g.name, newParentId, groupId)
    g.parentId = newParentId
    g.name = newName
    await save()
  }

  function isDescendantOf(ancestorId: string, targetId: string): boolean {
    const children = groups.value.filter(g => g.parentId === ancestorId)
    for (const child of children) {
      if (child.id === targetId) return true
      if (isDescendantOf(child.id, targetId)) return true
    }
    return false
  }

  function getChildGroups(parentId: string): ConnectionGroup[] {
    return groups.value.filter(g => g.parentId === parentId)
  }

  async function setConnectionGroup(connectionId: string, groupId: string | undefined) {
    const c = connections.value.find(c => c.id === connectionId)
    if (c) {
      c.groupId = groupId
      await save()
    }
  }

  async function setConnectionsGroup(connectionIds: string[], groupId: string | undefined) {
    for (const id of connectionIds) {
      const c = connections.value.find(c => c.id === id)
      if (c) {
        c.groupId = groupId
      }
    }
    await save()
  }

  // Move connections into targetGroupId and reorder within the connections
  // array so they land right before beforeId (or at the group's end when
  // beforeId is omitted). Preserves the relative order of the moved ids.
  async function moveConnections(connectionIds: string[], targetGroupId: string | undefined, beforeId?: string) {
    const moveSet = new Set(connectionIds)
    // Grab moved items in their current array order, apply the new group
    const moved = connections.value.filter(c => moveSet.has(c.id))
    if (moved.length === 0) return
    for (const c of moved) c.groupId = targetGroupId

    const rest = connections.value.filter(c => !moveSet.has(c.id))

    let insertAt: number
    if (beforeId && !moveSet.has(beforeId)) {
      const idx = rest.findIndex(c => c.id === beforeId)
      insertAt = idx >= 0 ? idx : rest.length
    } else {
      // Append after the last connection already in the target group
      let lastIdx = -1
      for (let i = 0; i < rest.length; i++) {
        if (rest[i].groupId === targetGroupId) lastIdx = i
      }
      insertAt = lastIdx >= 0 ? lastIdx + 1 : rest.length
    }

    rest.splice(insertAt, 0, ...moved)
    connections.value = rest
    await save()
  }

  // Reparent a group (reusing reparentGroup's cycle/name guards) and reorder
  // it within the groups array so it lands right before beforeId sibling
  // (or at the end of its new parent when beforeId is omitted).
  async function moveGroup(groupId: string, newParentId: string | undefined, beforeId?: string) {
    if (newParentId === groupId) return
    if (newParentId && isDescendantOf(groupId, newParentId)) return

    const g = groups.value.find(g => g.id === groupId)
    if (!g) return

    const newName = uniqueGroupName(g.name, newParentId, groupId)
    g.parentId = newParentId
    g.name = newName

    const rest = groups.value.filter(x => x.id !== groupId)
    let insertAt: number
    if (beforeId && beforeId !== groupId) {
      const idx = rest.findIndex(x => x.id === beforeId)
      insertAt = idx >= 0 ? idx : rest.length
    } else {
      let lastIdx = -1
      for (let i = 0; i < rest.length; i++) {
        if (rest[i].parentId === newParentId) lastIdx = i
      }
      insertAt = lastIdx >= 0 ? lastIdx + 1 : rest.length
    }
    rest.splice(insertAt, 0, g)
    groups.value = rest
    await save()
  }

  // ── Derived: tree structure ──

  const groupedConnections = computed<GroupedConnections>(() => {
    const nodeMap = new Map<string, GroupTreeNode>()
    for (const g of groups.value) {
      nodeMap.set(g.id, { group: g, connections: [], children: [] })
    }

    // Build tree: identify roots vs children
    const roots: GroupTreeNode[] = []
    for (const g of groups.value) {
      const node = nodeMap.get(g.id)!
      if (g.parentId && nodeMap.has(g.parentId)) {
        nodeMap.get(g.parentId)!.children.push(node)
      } else {
        roots.push(node)
      }
    }

    // Assign connections to their group nodes
    const ungrouped: ConnectionConfig[] = []
    for (const c of connections.value) {
      if (c.groupId && nodeMap.has(c.groupId)) {
        nodeMap.get(c.groupId)!.connections.push(c)
      } else {
        ungrouped.push(c)
      }
    }

    // Display order follows the underlying array order (manual sort).
    // No name-based sorting: drag & drop reorders the arrays directly.
    return { roots, ungrouped }
  })

  // Flat list of all group IDs (for Sidebar expand/collapse management)
  const allGroupIds = computed<string[]>(() => {
    const ids: string[] = []
    function collect(nodes: GroupTreeNode[]) {
      for (const node of nodes) {
        ids.push(node.group.id)
        collect(node.children)
      }
    }
    collect(groupedConnections.value.roots)
    return ids
  })

  // Listen for cross-window connection sync
  unsubConnectionsChanged?.()
  unsubConnectionsChanged =Events.On('store:connections:changed', (ev) => { const data: { groups?: ConnectionGroup[]; connections?: ConnectionConfig[] } = ev.data; 
    if (data) {
      // The other window wrote the disk, which still has our pending
      // deletions: keep them off the screen until they commit or undo.
      let conns = data.connections ?? connections.value
      let grps = data.groups ?? groups.value
      for (const op of deleteOps.values()) ({ conns, groups: grps } = applyDelete(op, conns, grps))
      if (data.groups) groups.value = grps
      if (data.connections) connections.value = conns
    }
   })

  function dispose() {
    unsubConnectionsChanged?.()
    unsubConnectionsChanged = null
  }

  return {
    connections,
    groups,
    loading,
    load,
    save,
    add,
    update,
    remove,
    removeMany,
    scheduleRemoveMany,
    scheduleDeleteGroup,
    undoDelete,
    pendingDeletes,
    addGroup,
    renameGroup,
    setGroupStyle,
    deleteGroup,
    reparentGroup,
    isDescendantOf,
    getChildGroups,
    setConnectionGroup,
    setConnectionsGroup,
    moveConnections,
    moveGroup,
    groupedConnections,
    allGroupIds,
    dispose
  }
})
