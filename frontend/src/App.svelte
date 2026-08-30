<script>
  import { onMount } from 'svelte';
  import { Background, BackgroundVariant, Controls, SvelteFlow } from '@xyflow/svelte';
  import '@xyflow/svelte/dist/style.css';
  import CodeCard from './CodeCard.svelte';

  let nodes = [];
  let edges = [];
  const nodeTypes = { code: CodeCard };
  let mapShell = null;

  let graph = null;
  let selectedId = '';
  let selectedIds = [];
  let view = 'changes';
  let mapTool = 'pan';
  let scope = 'all';
  let period = '30';
  let loading = true;
  let connected = false;
  let notice = '';
  let inspectorOpen = true;
  let timelineOpen = true;
  let testsPulledOut = false;
  let aiContext = null;
  let aiContextLoading = false;
  let marquee = null;
  let marqueeStart = null;
  let activityLog = [];
  const viewOptions = [
    { id: 'calls', label: 'What calls what', shortcut: '3' },
    { id: 'changes', label: 'Changes together', shortcut: '4' },
    { id: 'both', label: 'Both', shortcut: '5' }
  ];
  const toolOptions = [
    { id: 'pan', label: 'Pan', shortcut: '1' },
    { id: 'select', label: 'Area select', shortcut: '2' }
  ];
  const periodOptions = [
    { id: '30', label: '30d' },
    { id: '90', label: '90d' },
    { id: 'all', label: 'All' }
  ];
  const scopeOptions = [
    { id: 'all', label: 'All', shortcut: '6' },
    { id: 'frontend', label: 'Frontend', shortcut: '7' },
    { id: 'backend', label: 'Backend', shortcut: '8' },
    { id: 'tests', label: 'Tests', shortcut: '9' }
  ];
  const categoryOrder = ['frontend', 'backend', 'tests'];

  $: selected = nodes.find((node) => node.id === selectedId)?.data ?? graph?.nodes.find((node) => node.id === selectedId);
  $: selectedCards = selectedIds
    .map((id) => nodes.find((node) => node.id === id)?.data ?? graph?.nodes.find((node) => node.id === id))
    .filter(Boolean);
  $: selectedChangeCount = selectedCards.filter((node) => node.change).length;
  $: selectedCardsCommitCount = selectedCards.reduce((total, node) => total + activityCount(node.activity), 0);
  $: changedNodes = graph?.nodes.filter((node) => node.change) ?? [];
  $: otherChangeCount = graph?.otherChanges.length ?? 0;
  $: changedCount = changedNodes.length + otherChangeCount;
  $: sourceFileCount = scopedSourceNodes(graph?.nodes ?? []).length;
  $: connectionCount = edges.length;
  $: selectedCommitCount = activityCount(selected?.activity);
  $: timelineMax = Math.max(1, ...(graph?.activity ?? []).map((bucket) => bucket.count));
  $: dependsOn = relatedNodes('out');
  $: usedBy = relatedNodes('in');
  $: connectedFiles = [...dependsOn, ...usedBy].slice(0, 4);

  function reviewGraph(apiNodes, apiEdges) {
    let roots = apiNodes.filter((node) => node.isRoot);
    if (roots.length === 0 && apiNodes.length > 0) {
      const incoming = new Set(apiEdges.map((edge) => edge.to));
      roots = apiNodes.filter((node) => !incoming.has(node.id) && !node.id.endsWith('_test.go'));
      if (roots.length === 0) roots = [apiNodes[0]];
    }

    const visibleIds = new Set([
      ...roots.map((node) => node.id),
      ...apiNodes.filter((node) => node.change).map((node) => node.id)
    ]);
    if (testsPulledOut) {
      for (const node of apiNodes.filter((item) => nodeArea(item) === 'tests')) {
        visibleIds.add(node.id);
      }
    }
    const visibleNodes = apiNodes.filter((node) => visibleIds.has(node.id));
    return {
      nodes: visibleNodes,
      edges: collapseHiddenPaths(apiEdges, visibleIds)
    };
  }

  function collapseHiddenPaths(apiEdges, visibleIds) {
    const outgoing = new Map();
    const directPairs = new Set();
    const result = [];

    for (const edge of apiEdges) {
      const neighbors = outgoing.get(edge.from) ?? [];
      neighbors.push(edge);
      outgoing.set(edge.from, neighbors);

      if (visibleIds.has(edge.from) && visibleIds.has(edge.to)) {
        directPairs.add(`${edge.from}:${edge.to}`);
        result.push(edge);
      }
    }

    const indirectPairs = new Set();
    for (const source of visibleIds) {
      const queue = [...(outgoing.get(source) ?? [])];
      const visitedHidden = new Set();

      for (let index = 0; index < queue.length; index += 1) {
        const target = queue[index].to;
        if (visibleIds.has(target)) {
          const pair = `${source}:${target}`;
          if (source !== target && !directPairs.has(pair) && !indirectPairs.has(pair)) {
            indirectPairs.add(pair);
            result.push({ from: source, to: target, kind: 'indirect' });
          }
          continue;
        }
        if (visitedHidden.has(target)) continue;
        visitedHidden.add(target);
        queue.push(...(outgoing.get(target) ?? []));
      }
    }

    return result.sort((left, right) =>
      left.from.localeCompare(right.from) ||
      left.to.localeCompare(right.to) ||
      left.kind.localeCompare(right.kind)
    );
  }

  function visibleGraph(apiNodes, apiEdges) {
    const scoped = buildScopedGraph(apiNodes, apiEdges);
    if (view === 'calls' || view === 'both') return scoped;
    return reviewGraph(scoped.nodes, scoped.edges);
  }

  function buildScopedGraph(apiNodes, apiEdges) {
    const scopedNodes = scopedSourceNodes(apiNodes);
    const scopedIds = new Set(scopedNodes.map((node) => node.id));
    const scopedEdges = apiEdges.filter((edge) => scopedIds.has(edge.from) && scopedIds.has(edge.to));
    const resultNodes = [...scopedNodes];
    const resultEdges = [...scopedEdges];
    const categories = [...new Set(scopedNodes.map(nodeArea))].sort((left, right) => categoryRank(left) - categoryRank(right));

    for (const category of categories) {
      const parent = categoryNode(category);
      resultNodes.push(parent);
      for (const root of categoryRoots(scopedNodes, scopedEdges, category)) {
        resultEdges.push({ from: parent.id, to: root.id, kind: 'category' });
      }
    }

    if (scope === 'all' && categories.includes('frontend') && categories.includes('backend')) {
      resultEdges.push({ from: '__frontend__', to: '__backend__', kind: 'bridge' });
    }

    return { nodes: resultNodes, edges: resultEdges };
  }

  function scopedSourceNodes(apiNodes) {
    if (scope === 'all') return apiNodes;
    return apiNodes.filter((node) => nodeArea(node) === scope);
  }

  function nodeArea(node) {
    if (categoryOrder.includes(node.area)) return node.area;
    const id = String(node.id ?? '').toLowerCase();
    const language = String(node.language ?? '').toLowerCase();
    if (isTestFile(id)) return 'tests';
    if (
      id.startsWith('frontend/') ||
      ['svelte', 'javascript', 'typescript', 'css', 'html', 'vue'].includes(language)
    ) {
      return 'frontend';
    }
    return 'backend';
  }

  function isTestFile(id) {
    const normalized = id.replaceAll('\\', '/');
    return (
      normalized.endsWith('_test.go') ||
      /\.(test|spec)\.[^.]+$/.test(normalized) ||
      /(^|\/)(__tests__|tests?|specs?)(\/|$)/.test(normalized)
    );
  }

  function categoryRank(category) {
    const index = categoryOrder.indexOf(category);
    return index === -1 ? categoryOrder.length : index;
  }

  function categoryNode(category) {
    const childCount = scopedSourceNodes(graph?.nodes ?? []).filter((node) => nodeArea(node) === category).length;
    const details = {
      frontend: {
        id: '__frontend__',
        label: 'Frontend',
        description: `${childCount} client-side source files and UI entry points.`
      },
      backend: {
        id: '__backend__',
        label: 'Backend',
        description: `${childCount} server-side source files and analysis/map logic.`
      },
      tests: {
        id: '__tests__',
        label: 'Tests',
        description: `${childCount} test files and validation entry points.`
      }
    }[category] ?? {
      id: `__${category}__`,
      label: category,
      description: `${childCount} source files.`
    };
    return {
      id: details.id,
      label: details.label,
      language: 'Scope',
      kind: 'folder',
      description: details.description,
      isRoot: true,
      openable: false,
      area: category,
      activity: { commits30: 0, commits90: 0, commitsAll: 0, people: 0, recentCommits: [] }
    };
  }

  function categoryRoots(apiNodes, apiEdges, category) {
    const categoryNodes = apiNodes.filter((node) => nodeArea(node) === category);
    const categoryIds = new Set(categoryNodes.map((node) => node.id));
    const incoming = new Set(apiEdges.filter((edge) => categoryIds.has(edge.from) && categoryIds.has(edge.to)).map((edge) => edge.to));
    const roots = categoryNodes.filter((node) => node.isRoot || !incoming.has(node.id));
    return (roots.length ? roots : categoryNodes.slice(0, 4)).slice(0, 6);
  }

  function scopeNodeY(id) {
    const category = String(id).replace(/^__|__$/g, '');
    return Math.max(0, categoryRank(category)) * 150;
  }

  function layoutNodes(apiNodes, apiEdges) {
    const previousPositions = new Map(nodes.map((node) => [node.id, node.position]));
    const incoming = new Map(apiNodes.map((node) => [node.id, 0]));
    const outgoing = new Map(apiNodes.map((node) => [node.id, []]));
    const testOrder = apiNodes
      .filter((node) => nodeArea(node) === 'tests' && !String(node.id).startsWith('__'))
      .map((node) => node.id)
      .sort((left, right) => String(left).localeCompare(String(right)));

    for (const edge of apiEdges.filter((edge) => edge.kind !== 'bridge')) {
      incoming.set(edge.to, (incoming.get(edge.to) ?? 0) + 1);
      outgoing.get(edge.from)?.push(edge.to);
    }

    const roots = apiNodes.filter((node) => node.isRoot).map((node) => node.id);
    if (roots.length === 0) {
      roots.push(...apiNodes.filter((node) => incoming.get(node.id) === 0).map((node) => node.id));
    }
    if (roots.length === 0 && apiNodes[0]) roots.push(apiNodes[0].id);

    const depths = new Map();
    const queue = roots.map((id) => ({ id, depth: 0 }));
    for (let index = 0; index < queue.length; index += 1) {
      const current = queue[index];
      if (depths.has(current.id)) continue;
      depths.set(current.id, current.depth);
      for (const child of outgoing.get(current.id) ?? []) {
        if (!depths.has(child)) queue.push({ id: child, depth: current.depth + 1 });
      }
    }

    const fallbackDepth = Math.max(0, ...depths.values()) + 1;
    const nodesByDepth = new Map();
    for (const node of apiNodes) {
      const depth = depths.get(node.id) ?? fallbackDepth;
      const level = nodesByDepth.get(depth) ?? [];
      level.push(node);
      nodesByDepth.set(depth, level);
    }

    const result = [];
    const selectedSet = new Set(selectedIds);
    const horizontalGap = 260;
    const verticalGap = 128;
    for (const [depth, level] of [...nodesByDepth.entries()].sort(([left], [right]) => left - right)) {
      level.forEach((node, index) => {
        const isScopeNode = String(node.id).startsWith('__');
        const area = nodeArea(node);
        const testIndex = testOrder.indexOf(node.id);
        const pulledTestPosition = testsPulledOut && area === 'tests' && !isScopeNode
          ? { x: 360 + (testIndex % 6) * horizontalGap, y: 520 + Math.floor(testIndex / 6) * verticalGap }
          : null;
        result.push({
          id: node.id,
          type: 'code',
          data: { ...node, pulledOut: Boolean(pulledTestPosition) },
          selected: selectedSet.has(node.id),
          position: pulledTestPosition ?? previousPositions.get(node.id) ?? {
            x: isScopeNode ? 0 : (depth + 1) * horizontalGap,
            y: isScopeNode ? scopeNodeY(node.id) : index * verticalGap
          },
          ariaLabel: `${node.label}: ${node.description}`
        });
      });
    }
    return result;
  }

  function renderGraph(nextGraph = graph) {
    if (!nextGraph) return;
    const review = visibleGraph(nextGraph.nodes, nextGraph.edges);
    const visibleIds = new Set(review.nodes.map((node) => node.id));
    selectedIds = selectedIds.filter((id) => visibleIds.has(id));
    if (selectedId && !visibleIds.has(selectedId)) selectedId = '';
    if (!selectedId && selectedIds.length > 0) selectedId = selectedIds[0];
    if (!selectedId && review.nodes.length > 0) {
      selectedId = review.nodes.find((node) => node.change)?.id ?? review.nodes[0].id;
    }
    if (selectedId && !selectedIds.includes(selectedId)) {
      selectedIds = uniqueIds([selectedId, ...selectedIds]);
    }
    nodes = layoutNodes(review.nodes, review.edges);
    edges = review.edges.map((edge) => ({
      id: `${edge.from}:${edge.to}:${edge.kind}`,
      source: edge.from,
      target: edge.to,
      type: 'bezier',
      interactionWidth: 28,
      animated: testsPulledOut && edgeTouchesArea(edge, 'tests'),
      data: { kind: edge.kind },
      class: edgeClass(edge)
    }));
  }

  function setView(nextView) {
    view = nextView;
    renderGraph();
  }

  function setMapTool(nextTool) {
    mapTool = nextTool;
    marqueeStart = null;
    marquee = null;
    clearMarqueeListeners();
  }

  function setScope(nextScope) {
    scope = nextScope;
    selectedId = '';
    selectedIds = [];
    aiContext = null;
    renderGraph();
  }

  function handleShortcut(event) {
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || isTypingTarget(event.target)) return;
    const key = event.key;
    const tool = toolOptions.find((option) => option.shortcut === key);
    if (tool) {
      event.preventDefault();
      setMapTool(tool.id);
      return;
    }
    const viewOption = viewOptions.find((option) => option.shortcut === key);
    if (viewOption) {
      event.preventDefault();
      setView(viewOption.id);
      return;
    }
    const scopeOption = scopeOptions.find((option) => option.shortcut === key);
    if (scopeOption) {
      event.preventDefault();
      setScope(scopeOption.id);
    }
  }

  function isTypingTarget(target) {
    const tag = target?.tagName?.toLowerCase();
    return target?.isContentEditable || tag === 'input' || tag === 'textarea' || tag === 'select';
  }

  function edgeClass(edge) {
    const selected = new Set(selectedIds);
    return [
      edge.kind === 'indirect' ? 'indirect-edge' : edge.kind === 'bridge' ? 'bridge-edge' : edge.kind === 'category' ? 'category-edge' : 'code-edge',
      `${edgeArea(edge)}-thread`,
      selected.size > 0 && (selected.has(edge.from) || selected.has(edge.to)) ? 'active-edge' : ''
    ].filter(Boolean).join(' ');
  }

  function edgeArea(edge) {
    if (edge.kind === 'bridge') return 'bridge';
    const fromArea = nodeAreaById(edge.from);
    const toArea = nodeAreaById(edge.to);
    if (fromArea && fromArea === toArea) return fromArea;
    if (fromArea && toArea && fromArea !== toArea) {
      return fromArea === 'tests' || toArea === 'tests' ? 'tests' : 'mixed';
    }
    if (fromArea) return fromArea;
    if (toArea) return toArea;
    return 'mixed';
  }

  function edgeTouchesArea(edge, area) {
    return nodeAreaById(edge.from) === area || nodeAreaById(edge.to) === area;
  }

  function nodeAreaById(id) {
    if (!id || !graph) return '';
    const category = String(id).replace(/^__|__$/g, '');
    if (categoryOrder.includes(category)) return category;
    const node = graph.nodes.find((item) => item.id === id);
    return node ? nodeArea(node) : '';
  }

  function syncSelectionStyles() {
    const selected = new Set(selectedIds);
    nodes = nodes.map((node) => ({ ...node, selected: selected.has(node.id) }));
    edges = edges.map((edge) => ({
      ...edge,
      class: edgeClass({ from: edge.source, to: edge.target, kind: edge.data?.kind ?? 'imports' })
    }));
  }

  function uniqueIds(ids) {
    return [...new Set(ids.filter(Boolean))];
  }

  function addActivity(message) {
    activityLog = [
      { at: new Date().toLocaleTimeString(), message },
      ...activityLog
    ].slice(0, 8);
  }

  function formatGeneratedAt(value) {
    if (!value) return 'waiting';
    return relativeTime(value);
  }

  function activityCount(activity = null) {
    if (!activity) return 0;
    if (period === '90') return activity.commits90 ?? 0;
    if (period === 'all') return activity.commitsAll ?? 0;
    return activity.commits30 ?? 0;
  }

  function relatedNodes(direction) {
    if (!graph || !selectedId) return [];
    const byId = new Map(graph.nodes.map((node) => [node.id, node]));
    const ids = [];
    for (const edge of graph.edges) {
      if (direction === 'out' && edge.from === selectedId) ids.push(edge.to);
      if (direction === 'in' && edge.to === selectedId) ids.push(edge.from);
    }
    return [...new Set(ids)]
      .map((id) => byId.get(id))
      .filter(Boolean)
      .sort((left, right) => activityCount(right.activity) - activityCount(left.activity));
  }

  function relativeTime(value) {
    if (!value) return 'never';
    const seconds = Math.max(1, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) return 'just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes} min ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} hours ago`;
    const days = Math.floor(hours / 24);
    if (days < 30) return `${days} days ago`;
    const months = Math.floor(days / 30);
    if (months < 12) return `${months} months ago`;
    return `${Math.floor(months / 12)} years ago`;
  }

  function barHeight(count) {
    return `${Math.max(8, Math.round((count / timelineMax) * 112))}px`;
  }

  function pluralizeCommit(count) {
    return `${count} ${count === 1 ? 'commit' : 'commits'}`;
  }

  function authorBreakdown(authors = []) {
    if (authors.length === 0) return '';
    return authors
      .slice(0, 3)
      .map((author) => `${author.name} (${author.count})`)
      .join(', ');
  }

  function timelineLabel(bucket) {
    if (!bucket?.count) return '0 commits';
    const authors = authorBreakdown(bucket.authors ?? []);
    if (!authors) return pluralizeCommit(bucket.count);
    return `${pluralizeCommit(bucket.count)} by ${authors}`;
  }

  function relationWidth(node) {
    const max = Math.max(1, selectedCommitCount, ...connectedFiles.map((item) => activityCount(item.activity)));
    return `${Math.max(12, Math.round((activityCount(node.activity) / max) * 100))}%`;
  }

  function selectedLead() {
    if (!selected) return 'Select a file to inspect its role, activity, and connections.';
    if (selected.change) return selected.change.summary.changed;
    if (selected.activity?.commits30 > 0) return `Active source file with ${selected.activity.commits30} commits in the last 30 days.`;
    return selected.description;
  }

  async function loadGraph() {
    try {
      const response = await fetch('/api/graph', { cache: 'no-store' });
      if (!response.ok) throw new Error(`Graph request failed (${response.status})`);
      const nextGraph = await response.json();
      const previousRevision = graph?.revision;
      graph = nextGraph;
      renderGraph(nextGraph);
      loading = false;
      notice = '';
      if (previousRevision !== nextGraph.revision) {
        addActivity(`Loaded repository map revision ${nextGraph.revision}`);
      }
    } catch (error) {
      loading = false;
      notice = error instanceof Error ? error.message : String(error);
    }
  }

  async function openNode(id) {
    if (!id) return;
    try {
      const response = await fetch('/api/open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not open file');
      notice = `Opened ${id}`;
    } catch (error) {
      notice = error instanceof Error ? error.message : String(error);
    }
  }

  function selectedContextIds() {
    if (selectedIds.length > 0) return selectedIds;
    if (selectedId) return [selectedId];
    return [];
  }

  async function buildAIContext() {
    const ids = selectedContextIds();
    if (ids.length === 0) {
      notice = 'Select one or more files first';
      return;
    }
    aiContextLoading = true;
    try {
      const response = await fetch('/api/context', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids })
      });
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not build context');
      aiContext = await response.json();
      notice = '';
    } catch (error) {
      notice = error instanceof Error ? error.message : String(error);
    } finally {
      aiContextLoading = false;
    }
  }

  async function copyAIContext() {
    if (!aiContext?.prompt) return;
    try {
      await navigator.clipboard.writeText(aiContext.prompt);
      notice = '';
    } catch (error) {
      notice = error instanceof Error ? error.message : String(error);
    }
  }

  function handleNodeClick({ node, event }) {
    const multi = Boolean(event?.shiftKey || event?.metaKey || event?.ctrlKey);
    if (multi) {
      selectedIds = selectedIds.includes(node.id)
        ? selectedIds.filter((id) => id !== node.id)
        : uniqueIds([...selectedIds, node.id]);
      selectedId = selectedIds.includes(node.id) ? node.id : selectedIds[0] ?? '';
    } else {
      selectedId = node.id;
      selectedIds = [node.id];
    }
    aiContext = null;
    inspectorOpen = true;
    syncSelectionStyles();
    if ('detail' in event && event.detail >= 2) openNode(node.id);
  }

  function handleNodeContextMenu({ node, event }) {
    if (node.id !== '__tests__') return;
    event?.preventDefault();
    testsPulledOut = !testsPulledOut;
    nodes = [];
    renderGraph();
  }

  function handleMarqueePointerDown(event) {
    if (mapTool !== 'select' || event.button !== 0 || !mapShell || shouldSkipMarquee(event.target)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    const point = mapShellPoint(event);
    marqueeStart = { x: point.x, y: point.y, clientX: event.clientX, clientY: event.clientY };
    marquee = { left: point.x, top: point.y, width: 0, height: 0 };
    window.addEventListener('pointermove', handleMarqueePointerMove, { capture: true });
    window.addEventListener('pointerup', handleMarqueePointerUp, { capture: true, once: true });
  }

  function handleMarqueePointerMove(event) {
    if (!marqueeStart) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    updateMarquee(event);
  }

  function handleMarqueePointerUp(event) {
    if (!marqueeStart) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    updateMarquee(event);
    selectNodesInMarquee(event);
    clearMarqueeListeners();
    marqueeStart = null;
    marquee = null;
  }

  function updateMarquee(event) {
    const point = mapShellPoint(event);
    marquee = {
      left: Math.min(marqueeStart.x, point.x),
      top: Math.min(marqueeStart.y, point.y),
      width: Math.abs(point.x - marqueeStart.x),
      height: Math.abs(point.y - marqueeStart.y)
    };
  }

  function selectNodesInMarquee(event) {
    const moved = Math.hypot(event.clientX - marqueeStart.clientX, event.clientY - marqueeStart.clientY);
    const rect = {
      left: Math.min(marqueeStart.clientX, event.clientX),
      right: Math.max(marqueeStart.clientX, event.clientX),
      top: Math.min(marqueeStart.clientY, event.clientY),
      bottom: Math.max(marqueeStart.clientY, event.clientY)
    };
    const ids = moved < 4 ? nodeIdAtPoint(event.clientX, event.clientY) : nodeIdsInRect(rect);
    selectedIds = Array.isArray(ids) ? ids : ids ? [ids] : [];
    selectedId = selectedIds.at(-1) ?? '';
    aiContext = null;
    if (selectedIds.length > 0) inspectorOpen = true;
    syncSelectionStyles();
  }

  function nodeIdsInRect(rect) {
    return [...mapShell.querySelectorAll('.svelte-flow__node[data-id]')]
      .filter((element) => !element.dataset.id?.startsWith('__') && rectsIntersect(rect, element.getBoundingClientRect()))
      .map((element) => element.dataset.id)
      .filter(Boolean);
  }

  function nodeIdAtPoint(clientX, clientY) {
    const element = document
      .elementsFromPoint(clientX, clientY)
      .find((item) => item.classList?.contains('svelte-flow__node') && !item.dataset.id?.startsWith('__'));
    return element?.dataset.id ?? '';
  }

  function rectsIntersect(a, b) {
    return a.left <= b.right && a.right >= b.left && a.top <= b.bottom && a.bottom >= b.top;
  }

  function mapShellPoint(event) {
    const bounds = mapShell.getBoundingClientRect();
    return { x: event.clientX - bounds.left, y: event.clientY - bounds.top };
  }

  function shouldSkipMarquee(target) {
    return Boolean(target?.closest?.('.svelte-flow__controls, .map-legend'));
  }

  function clearMarqueeListeners() {
    window.removeEventListener('pointermove', handleMarqueePointerMove, { capture: true });
    window.removeEventListener('pointerup', handleMarqueePointerUp, { capture: true });
  }

  function handleSelectionChange({ nodes: selectedNodes }) {
    const nextIds = selectedNodes.map((node) => node.id);
    if (nextIds.join('\0') === selectedIds.join('\0')) return;
    selectedIds = nextIds;
    selectedId = selectedIds.includes(selectedId) ? selectedId : selectedIds.at(-1) ?? '';
    aiContext = null;
    if (selectedIds.length > 0) inspectorOpen = true;
    syncSelectionStyles();
  }

  onMount(() => {
    loadGraph();
    mapShell?.addEventListener('pointerdown', handleMarqueePointerDown, { capture: true });
    window.addEventListener('keydown', handleShortcut);
    const events = new EventSource('/events');
    events.addEventListener('ready', () => {
      connected = true;
      addActivity('Connected to the live repository watcher');
    });
    events.addEventListener('graph-changed', (event) => {
      const payload = JSON.parse(event.data);
      connected = true;
      addActivity(`Detected repository changes for revision ${payload.revision}`);
      loadGraph();
    });
    events.addEventListener('analysis-error', (event) => {
      const payload = JSON.parse(event.data);
      notice = `Keeping the last valid map: ${payload.error}`;
      addActivity(`Analysis error: ${payload.error}`);
    });
    events.onerror = () => connected = false;
    return () => {
      events.close();
      mapShell?.removeEventListener('pointerdown', handleMarqueePointerDown, { capture: true });
      window.removeEventListener('keydown', handleShortcut);
      clearMarqueeListeners();
    };
  });
</script>

<svelte:head>
  <meta name="description" content="A live, local map of the current Git repository" />
  <title>{graph?.repository ?? 'codemap'} · live map</title>
</svelte:head>

<main>
  <header>
    <div class="brand">
      <span class="mark"></span>
      <strong>codemap</strong>
      <span>{graph?.repository ?? 'loading repository'}</span>
      <em>{graph?.baseRevision ? `${graph.branch} vs ${graph.baseRevision}` : graph?.branch ?? 'main'}</em>
    </div>
    <div class="sync" class:online={connected}>
      <span>last sync {formatGeneratedAt(graph?.generatedAt)}</span><i></i>
    </div>
  </header>

  <div class="dashboard" class:inspector-closed={!inspectorOpen}>
    <section class="left-panel" class:timeline-collapsed={!timelineOpen}>
      <nav class="toolbar" aria-label="Map controls">
        <div class="control-group tool-filter">
          <span>Tool</span>
          <div>
            {#each toolOptions as option}
              <button class:active={mapTool === option.id} onclick={() => setMapTool(option.id)}><span>{option.label}</span><kbd>{option.shortcut}</kbd></button>
            {/each}
          </div>
        </div>
        <div class="control-group">
          <span>Show</span>
          <div>
            {#each viewOptions as option}
              <button class:active={view === option.id} onclick={() => setView(option.id)}><span>{option.label}</span><kbd>{option.shortcut}</kbd></button>
            {/each}
          </div>
        </div>
        <div class="control-group scope-filter">
          <span>Scope</span>
          <div>
            {#each scopeOptions as option}
              <button class:active={scope === option.id} onclick={() => setScope(option.id)}><span>{option.label}</span><kbd>{option.shortcut}</kbd></button>
            {/each}
          </div>
        </div>
        <div class="control-group activity-filter">
          <span>Change activity</span>
          <div>
            {#each periodOptions as option}
              <button class:active={period === option.id} onclick={() => period = option.id}>{option.label}</button>
            {/each}
          </div>
        </div>
      </nav>

      {#if notice}<div class="notice" role="status">{notice}</div>{/if}

      <section bind:this={mapShell} class="map-shell" class:select-mode={mapTool === 'select'} aria-label="Repository code map">
        <div class="map-legend" aria-label="Connection colors">
          <span><i class="frontend"></i>frontend</span>
          <span><i class="backend"></i>backend</span>
          <span><i class="tests"></i>tests</span>
          <span><i class="mixed"></i>cross</span>
          <span><i class="related"></i>collapsed path</span>
        </div>
        {#if loading}
          <div class="loading">Building the repository map...</div>
        {:else if nodes.length === 0}
          <div class="loading">No supported source files found.</div>
        {:else}
          {#if marquee}
            <div class="marquee" style={`left: ${marquee.left}px; top: ${marquee.top}px; width: ${marquee.width}px; height: ${marquee.height}px`}></div>
          {/if}
          <SvelteFlow
            bind:nodes
            bind:edges
            {nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.16, maxZoom: 0.85 }}
            minZoom={0.12}
            maxZoom={1.8}
            onnodeclick={handleNodeClick}
            onnodecontextmenu={handleNodeContextMenu}
            onselectionchange={handleSelectionChange}
            panOnDrag={mapTool === 'pan'}
            selectionOnDrag={false}
            nodesDraggable={mapTool === 'pan'}
            nodesConnectable={false}
            deleteKey={null}
            proOptions={{ hideAttribution: true }}
          >
            <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="#172533" />
            <Controls position="bottom-left" />
          </SvelteFlow>
        {/if}
      </section>

      <section class="timeline" aria-label="Commits over time">
        <div class="timeline-head">
          <span>Commits over time</span>
          <div>
            <em>{graph?.activity?.length ?? 0} weeks · live local Git history</em>
            <button
              aria-label={timelineOpen ? 'Collapse commit timeline' : 'Expand commit timeline'}
              title={timelineOpen ? 'Collapse commit timeline' : 'Expand commit timeline'}
              onclick={() => timelineOpen = !timelineOpen}
            >
              {timelineOpen ? 'Hide' : 'Show'}
            </button>
          </div>
        </div>
        {#if timelineOpen}
          <div class="bars">
            {#each graph?.activity ?? [] as bucket, index}
              <button
                class:hot={index >= (graph?.activity?.length ?? 0) - 4}
                aria-label={timelineLabel(bucket)}
                title={timelineLabel(bucket)}
                style={`height: ${barHeight(bucket.count)}`}
              >
                <span>{timelineLabel(bucket)}</span>
              </button>
            {/each}
          </div>
        {/if}
      </section>
    </section>

    {#if inspectorOpen}
    <aside>
      <div class="inspector-top">
        <p class="eyebrow">{selected ? 'Building the map' : 'Repository'}</p>
        <button class="close-inspector" aria-label="Close details panel" title="Close details panel" onclick={() => inspectorOpen = false}>x</button>
      </div>
      {#if selectedCards.length > 1}
        <h1>{selectedCards.length} files selected</h1>
        <p class="lead">{selectedChangeCount} changed files and {selectedCardsCommitCount} commits in the selected activity window.</p>
        <div class="ai-actions">
          <button onclick={buildAIContext} disabled={aiContextLoading}>{aiContextLoading ? 'Building context...' : 'Understand selection'}</button>
          <button onclick={copyAIContext} disabled={!aiContext?.prompt}>Copy AI context</button>
        </div>
        <div class="selection-list">
          {#each selectedCards as item}
            <button onclick={() => { selectedId = item.id; selectedIds = [item.id]; aiContext = null; syncSelectionStyles(); }}>
              <span>{item.label}</span>
              <em>{item.change?.status ?? `${activityCount(item.activity)} commits`}</em>
              <code>{item.id}</code>
            </button>
          {/each}
        </div>
      {:else if selected}
        <h1>{selected.label}</h1>
        <code>{selected.id}</code>
        <p class="lead">{selectedLead()}</p>
        <div class="ai-actions">
          <button onclick={buildAIContext} disabled={aiContextLoading}>{aiContextLoading ? 'Building context...' : 'Understand component'}</button>
          <button onclick={copyAIContext} disabled={!aiContext?.prompt}>Copy AI context</button>
        </div>

        <div class="stat-grid">
          <div><strong>{selectedCommitCount}</strong><span>commits · {period === 'all' ? 'all time' : `${period} days`}</span></div>
          <div><strong>{selected.activity?.people ?? 0}</strong><span>people touching it</span></div>
          <div><strong>{relativeTime(selected.activity?.lastChangedAt)}</strong><span>last change</span></div>
        </div>

        {#if connectedFiles.length}
          <h3>Usually changes with</h3>
          <div class="relation-list">
            {#each connectedFiles as item}
              <button onclick={() => { selectedId = item.id; selectedIds = [item.id]; aiContext = null; renderGraph(); }}>
                <span>{item.label}</span>
                <em>{activityCount(item.activity)} commits</em>
                <b><i style={`width: ${relationWidth(item)}`}></i></b>
              </button>
            {/each}
          </div>
          <p class="muted">These are directly connected files in the local code graph.</p>
        {/if}

        {#if selected.change}
          <h3>Previous vs now</h3>
          <div class="summary-block">
            <strong>Previously</strong><p>{selected.change.summary.previous}</p>
            <strong>Now</strong><p>{selected.change.summary.current}</p>
            <strong>Changed</strong><p>{selected.change.summary.changed}</p>
            <strong>Impact</strong><p>{selected.change.summary.impact}</p>
          </div>
          <div class="change-summary status-{selected.change.status.toLowerCase()}">
            <strong>{selected.change.status}</strong>
            <span>+{selected.change.additions} / -{selected.change.deletions}</span>
            <span>first change: line {selected.change.firstChangedLine}</span>
          </div>
        {/if}

        <h3>Recent commits</h3>
        <div class="commit-list">
          {#each selected.activity?.recentCommits ?? [] as commit}
            <div><b>{commit.hash}</b><span>{commit.message}</span><em>{commit.author || 'unknown'} · {relativeTime(commit.when)}</em></div>
          {:else}
            <p class="muted">No recent commits found for this file.</p>
          {/each}
        </div>

        <h3>Depends on</h3>
        <div class="chips">
          {#each dependsOn.slice(0, 6) as item}<button onclick={() => { selectedId = item.id; selectedIds = [item.id]; aiContext = null; renderGraph(); }}>{item.label}</button>{:else}<span>None detected</span>{/each}
        </div>
        <h3>Used by</h3>
        <div class="chips">
          {#each usedBy.slice(0, 6) as item}<button onclick={() => { selectedId = item.id; selectedIds = [item.id]; aiContext = null; renderGraph(); }}>{item.label}</button>{:else}<span>None detected</span>{/each}
        </div>

        <button class="open-button" disabled={!selected.openable} onclick={() => openNode(selected.id)}>Open in VS Code</button>
      {:else}
        <h1>{graph?.repository ?? 'Loading'}</h1>
        <p class="lead">Select a file to inspect activity, recent commits, and code connections.</p>
      {/if}
      {#if aiContext}
        <h3>AI context</h3>
        <div class="ai-context">
          <div><strong>{aiContext.title}</strong><span>{aiContext.fileCount} files{aiContext.truncated ? ' · truncated' : ''}</span></div>
          <pre>{aiContext.prompt}</pre>
        </div>
      {/if}
    </aside>
    {/if}
  </div>
</main>

<style>
  :global(*) { box-sizing: border-box; }
  :global(html) { height: 100%; overflow: hidden; background: #070d14; }
  :global(body) { margin: 0; min-width: 320px; height: 100%; overflow: hidden; color: #dce6ef; font-family: Inter, ui-sans-serif, system-ui, sans-serif; background: #070d14; }
  :global(button), :global(input) { font: inherit; }
  main { height: 100vh; height: 100dvh; min-height: 0; overflow: hidden; display: flex; flex-direction: column; }
  header { flex: 0 0 52px; height: 52px; padding: 0 18px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid #172332; background: #090f17; }
  .brand, .sync, .toolbar, .control-group, .control-group > div, .map-legend { display: flex; align-items: center; }
  .brand { gap: 10px; }
  .brand strong { font-size: 0.9rem; letter-spacing: 0.02em; }
  .brand > span:last-of-type { color: #607184; font: 0.78rem ui-monospace, Consolas, monospace; }
  .brand em { padding: 5px 9px; border: 1px solid #1a2a3a; border-radius: 7px; color: #9dacbb; background: #0c1520; font: 700 0.72rem ui-monospace, Consolas, monospace; font-style: normal; }
  .mark { width: 10px; height: 10px; border-radius: 3px; background: #ff9f43; }
  .sync { gap: 7px; color: #708094; font-size: 0.72rem; }
  .sync i { width: 6px; height: 6px; border-radius: 50%; background: #f59e0b; }
  .sync.online i { background: #35d07f; box-shadow: 0 0 14px rgba(53, 208, 127, 0.7); }
  .dashboard { min-height: 0; flex: 1 1 auto; overflow: hidden; display: grid; grid-template-columns: minmax(0, 1fr) minmax(430px, 490px); }
  .dashboard.inspector-closed { grid-template-columns: minmax(0, 1fr); }
  .left-panel { position: relative; min-width: 0; min-height: 0; display: grid; grid-template-rows: 46px minmax(0, 1fr) 158px; border-right: 1px solid #172332; overflow: hidden; }
  .left-panel.timeline-collapsed { grid-template-rows: 46px minmax(0, 1fr) 42px; }
  .dashboard.inspector-closed .left-panel { border-right: 0; }
  .toolbar { gap: 12px; padding: 0 12px; border-bottom: 1px solid #121d2a; background: #080e15; overflow-x: auto; }
  .control-group { gap: 8px; }
  .control-group > span, .timeline-head span, .eyebrow, h3 { color: #65768a; font: 800 0.62rem ui-monospace, Consolas, monospace; letter-spacing: 0.14em; text-transform: uppercase; white-space: nowrap; }
  .control-group > div { gap: 2px; padding: 3px; border: 1px solid #182839; border-radius: 8px; background: #0b141f; }
  .control-group button { border: 0; border-radius: 6px; padding: 6px 8px; display: flex; align-items: center; gap: 7px; color: #8293a5; background: transparent; cursor: pointer; font-size: 0.72rem; font-weight: 750; line-height: 1; white-space: nowrap; }
  .control-group button.active { color: #dbe6ef; background: #223247; }
  .control-group kbd { min-width: 1ch; color: #5f7286; font: 800 0.62rem ui-monospace, Consolas, monospace; }
  .control-group button.active kbd { color: #a9c3dc; }
  .tool-filter, .scope-filter, .activity-filter { margin-left: 4px; padding-left: 12px; border-left: 1px solid #182635; }
  .notice { position: absolute; left: 16px; top: 58px; z-index: 8; max-width: min(520px, calc(100% - 32px)); padding: 8px 10px; border: 1px solid #26394d; border-radius: 8px; color: #c7d6e4; background: rgba(9, 15, 23, 0.94); box-shadow: 0 12px 28px rgba(0, 0, 0, 0.26); font-size: 0.78rem; pointer-events: none; }
  .map-shell { position: relative; min-height: 0; background: radial-gradient(circle at 1px 1px, #132130 1px, transparent 0) 0 0 / 36px 36px, #0b1119; overflow: hidden; }
  .map-shell.select-mode { cursor: crosshair; }
  .marquee { position: absolute; z-index: 7; border: 1px dashed #8aa2ff; border-radius: 4px; background: rgba(91, 112, 255, 0.12); box-shadow: 0 0 0 1px rgba(138, 162, 255, 0.18), inset 0 0 0 1px rgba(255, 255, 255, 0.08); pointer-events: none; }
  .loading { position: absolute; inset: 0; display: grid; place-items: center; color: #708898; z-index: 2; }
  .map-legend { position: absolute; left: 16px; top: 16px; z-index: 4; gap: 12px; padding: 8px 10px; border: 1px solid #17283a; border-radius: 8px; color: #76889b; background: rgba(8, 14, 21, 0.84); font-size: 0.72rem; pointer-events: none; }
  .map-legend span { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
  .map-legend i { width: 18px; height: 3px; border-radius: 999px; }
  .map-legend .frontend { background: #37c4ee; }
  .map-legend .backend { background: #d49a3c; }
  .map-legend .tests { background: #a78bfa; }
  .map-legend .mixed { background: #6ee7b7; }
  .map-legend .related { background: repeating-linear-gradient(90deg, #7d8794 0 5px, transparent 5px 9px); }
  .timeline { min-height: 0; padding: 13px 18px 16px; border-top: 1px solid #172332; background: #081018; overflow: hidden; }
  .timeline-collapsed .timeline { padding: 11px 18px; }
  .timeline-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
  .timeline-collapsed .timeline-head { margin-bottom: 0; }
  .timeline-head div { display: flex; align-items: center; gap: 10px; min-width: 0; }
  .timeline-head em { color: #596b7f; font: 0.7rem ui-monospace, Consolas, monospace; font-style: normal; }
  .timeline-head button { border: 1px solid #1d3143; border-radius: 7px; padding: 5px 8px; color: #9eb2c4; background: #0d1824; font-size: 0.68rem; font-weight: 800; line-height: 1; cursor: pointer; }
  .timeline-head button:hover { color: #edf5fb; background: #17283a; }
  .bars { height: 96px; display: grid; grid-template-columns: repeat(24, minmax(10px, 1fr)); align-items: end; gap: 6px; }
  .bars button { min-width: 0; border: 0; border-radius: 5px 5px 2px 2px; background: #1a2634; cursor: pointer; position: relative; }
  .bars button.hot { background: linear-gradient(180deg, #ff9941, #e7a645); }
  .bars button span { position: absolute; left: 50%; bottom: calc(100% + 6px); z-index: 5; width: max-content; max-width: 220px; transform: translateX(-50%); padding: 5px 7px; border: 1px solid #24364a; border-radius: 5px; background: #0b141e; color: #a8b8c8; font-size: 0.65rem; line-height: 1.25; opacity: 0; pointer-events: none; box-shadow: 0 8px 20px rgba(0, 0, 0, 0.24); }
  .bars button:hover span { opacity: 1; }
  aside { min-width: 0; min-height: 0; overflow-y: auto; background: #080e15; }
  aside > * { margin-left: 22px; margin-right: 22px; }
  .inspector-top { position: sticky; top: 0; z-index: 3; display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 0; padding: 14px 18px 8px 22px; background: linear-gradient(180deg, #080e15 72%, rgba(8, 14, 21, 0)); }
  .inspector-top .eyebrow { margin: 0; }
  .close-inspector { flex: 0 0 auto; width: 28px; height: 28px; display: grid; place-items: center; border: 1px solid #1d3143; border-radius: 7px; color: #8fa2b5; background: #0b1520; font: 800 0.86rem ui-monospace, Consolas, monospace; line-height: 1; cursor: pointer; }
  .close-inspector:hover { color: #edf5fb; background: #17283a; }
  aside h1 { margin: 0 22px 4px; color: #eff5fb; font-size: 1.25rem; letter-spacing: 0; overflow-wrap: anywhere; }
  aside code { display: block; color: #607184; overflow-wrap: anywhere; font-size: 0.76rem; }
  .lead { margin-top: 14px; margin-bottom: 18px; color: #a6b4c3; font-size: 0.86rem; line-height: 1.38; }
  .ai-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-bottom: 16px; }
  .ai-actions button { min-width: 0; border: 1px solid #1d3143; border-radius: 8px; padding: 9px 10px; color: #dce7ef; background: #102033; font-size: 0.76rem; font-weight: 800; cursor: pointer; }
  .ai-actions button:first-child { color: #061018; border-color: #6ee7f9; background: #67e8f9; }
  .ai-actions button:hover:not(:disabled) { filter: brightness(1.08); }
  .ai-context { display: grid; gap: 10px; padding: 11px; border: 1px solid #1d3143; border-radius: 8px; background: #0b141e; }
  .ai-context div { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; }
  .ai-context strong { min-width: 0; color: #e5eef7; font-size: 0.8rem; overflow-wrap: anywhere; }
  .ai-context span { flex: 0 0 auto; color: #8395a8; font: 0.68rem ui-monospace, Consolas, monospace; }
  .ai-context pre { max-height: 320px; margin: 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; color: #aebed0; font: 0.68rem/1.45 ui-monospace, Consolas, monospace; }
  .selection-list { display: grid; gap: 8px; margin-top: 12px; }
  .selection-list button { min-width: 0; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 4px 10px; padding: 10px 11px; border: 1px solid #1d3143; border-radius: 8px; color: #dce7ef; background: #0b1520; text-align: left; cursor: pointer; }
  .selection-list button:hover { border-color: #3a6f88; background: #101d2a; }
  .selection-list span { min-width: 0; overflow-wrap: anywhere; font-size: 0.82rem; font-weight: 800; }
  .selection-list em { color: #f3a33f; font: 800 0.7rem ui-monospace, Consolas, monospace; font-style: normal; white-space: nowrap; }
  .selection-list code { grid-column: 1 / -1; margin: 0; font-size: 0.68rem; }
  .stat-grid { margin: 0; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-top: 1px solid #172332; border-bottom: 1px solid #172332; }
  .stat-grid div { min-width: 0; padding: 16px 18px; border-right: 1px solid #172332; }
  .stat-grid div:last-child { border-right: 0; }
  .stat-grid strong { display: block; color: #f58a39; font-size: 1.32rem; }
  .stat-grid span { display: block; margin-top: 2px; color: #6f7f91; font-size: 0.7rem; line-height: 1.28; }
  h3 { margin-top: 20px; margin-bottom: 10px; }
  .relation-list { display: grid; gap: 9px; }
  .relation-list button { border: 0; padding: 0; display: grid; grid-template-columns: 1fr auto; gap: 6px 12px; color: #dbe4ed; background: transparent; text-align: left; cursor: pointer; }
  .relation-list em { color: #c3913b; font: 700 0.77rem ui-monospace, Consolas, monospace; font-style: normal; }
  .relation-list b { grid-column: 1 / -1; height: 5px; overflow: hidden; border-radius: 999px; background: #162330; }
  .relation-list i { display: block; height: 100%; border-radius: inherit; background: #c99840; }
  .muted { color: #728296; font-size: 0.75rem; line-height: 1.35; }
  .summary-block { display: grid; gap: 5px; padding: 11px; border: 1px solid #172637; border-radius: 8px; background: #0b141e; }
  .summary-block strong { color: #dce7ef; font-size: 0.72rem; }
  .summary-block p { margin: 0 0 5px; color: #8799aa; font-size: 0.75rem; line-height: 1.35; }
  .change-summary { display: grid; grid-template-columns: auto 1fr; gap: 3px 10px; margin-top: 12px; padding: 10px; border: 1px solid #5d461c; border-radius: 8px; background: #18160f; color: #b8a77c; font-size: 0.72rem; }
  .change-summary strong { grid-row: span 2; align-self: center; color: #fbbf24; font-size: 1.1rem; }
  .change-summary.status-a { border-color: #1f5a42; background: #0e1d18; }
  .change-summary.status-a strong { color: #4ade80; }
  .change-summary.status-d { border-color: #61303c; background: #211117; }
  .change-summary.status-d strong { color: #fb7185; }
  .commit-list { display: grid; gap: 10px; }
  .commit-list div { display: grid; grid-template-columns: 58px 1fr; gap: 3px 12px; }
  .commit-list b { color: #35b6d7; font: 800 0.82rem ui-monospace, Consolas, monospace; }
  .commit-list span { color: #c1ccd7; font-size: 0.78rem; }
  .commit-list em { grid-column: 2; color: #67798c; font-size: 0.7rem; font-style: normal; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chips button, .chips span { border: 1px solid #1d3143; border-radius: 7px; padding: 6px 8px; color: #92a4b5; background: #0b1520; font: 0.72rem ui-monospace, Consolas, monospace; }
  .chips button { cursor: pointer; }
  .open-button { width: calc(100% - 44px); margin: 22px 22px; border: 0; border-radius: 8px; padding: 9px 12px; color: #061018; background: #67e8f9; font-weight: 800; cursor: pointer; }
  button:disabled { opacity: 0.45; cursor: not-allowed; }
  :global(.svelte-flow) { background: transparent; }
  :global(.svelte-flow__node) { transition: transform 260ms cubic-bezier(0.2, 0.8, 0.2, 1); }
  :global(.svelte-flow__edge-path) { stroke: rgba(73, 96, 114, 0.3); stroke-width: 1.4; stroke-linecap: round; stroke-linejoin: round; filter: none; }
  :global(.svelte-flow__edge.frontend-thread .svelte-flow__edge-path) { stroke: rgba(55, 196, 238, 0.5); }
  :global(.svelte-flow__edge.backend-thread .svelte-flow__edge-path) { stroke: rgba(212, 154, 60, 0.5); }
  :global(.svelte-flow__edge.tests-thread .svelte-flow__edge-path) { stroke: rgba(167, 139, 250, 0.58); }
  :global(.svelte-flow__edge.mixed-thread .svelte-flow__edge-path) { stroke: rgba(110, 231, 183, 0.46); }
  :global(.svelte-flow__edge.category-edge .svelte-flow__edge-path) { stroke-width: 1.8; }
  :global(.svelte-flow__edge.bridge-edge .svelte-flow__edge-path) { stroke: #6ee7b7; stroke-width: 2.3; filter: drop-shadow(0 0 6px rgba(110, 231, 183, 0.32)); }
  :global(.svelte-flow__edge.indirect-edge .svelte-flow__edge-path) { stroke: rgba(151, 122, 76, 0.34); stroke-width: 1.3; stroke-dasharray: 7 10; }
  :global(.svelte-flow__edge.active-edge .svelte-flow__edge-path) { stroke-width: 2.7; }
  :global(.svelte-flow__edge.frontend-thread.active-edge .svelte-flow__edge-path) { stroke: #37c4ee; filter: drop-shadow(0 0 7px rgba(55, 196, 238, 0.42)); }
  :global(.svelte-flow__edge.backend-thread.active-edge .svelte-flow__edge-path) { stroke: #d49a3c; filter: drop-shadow(0 0 7px rgba(212, 154, 60, 0.38)); }
  :global(.svelte-flow__edge.tests-thread.active-edge .svelte-flow__edge-path) { stroke: #a78bfa; filter: drop-shadow(0 0 7px rgba(167, 139, 250, 0.42)); }
  :global(.svelte-flow__edge.mixed-thread.active-edge .svelte-flow__edge-path), :global(.svelte-flow__edge.bridge-edge.active-edge .svelte-flow__edge-path) { stroke: #6ee7b7; filter: drop-shadow(0 0 7px rgba(110, 231, 183, 0.36)); }
  :global(.svelte-flow__edge.indirect-edge.active-edge .svelte-flow__edge-path) { filter: drop-shadow(0 0 7px currentColor); }
  :global(.svelte-flow__edge-text), :global(.svelte-flow__edge-textbg) { display: none; }
  :global(.svelte-flow__selection) { border-color: #7dd3fc; background: rgba(55, 196, 238, 0.11); }
  :global(.svelte-flow__controls) { border: 1px solid #1d3143; border-radius: 8px; overflow: hidden; box-shadow: none; }
  :global(.svelte-flow__controls-button) { border: 0; border-bottom: 1px solid #1d3143; color: #a7bfce; background: #0f1b27; }
  :global(.svelte-flow__controls-button:hover) { background: #17293a; }
  @media (max-width: 980px) {
    .dashboard { grid-template-columns: 1fr; }
    aside { border-top: 1px solid #172332; }
    .left-panel { border-right: 0; }
    .toolbar { overflow-x: auto; }
  }
  @media (max-width: 680px) {
    header { align-items: flex-start; height: auto; padding: 16px; gap: 14px; flex-direction: column; }
    .left-panel { grid-template-rows: auto 64vh 180px; }
    .left-panel.timeline-collapsed { grid-template-rows: auto 64vh 42px; }
    .toolbar { align-items: flex-start; flex-direction: column; padding: 14px 16px; }
    .tool-filter, .scope-filter, .activity-filter { margin-left: 0; padding-left: 0; border-left: 0; }
    .map-legend { display: none; }
    .stat-grid { grid-template-columns: 1fr; }
    .stat-grid div { border-right: 0; border-bottom: 1px solid #172332; }
  }
</style>
