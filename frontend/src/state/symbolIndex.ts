// Symbol extraction via web-tree-sitter (WASM) using the prebuilt
// tree-sitter-wasms bundle (go/typescript/tsx/javascript grammars).
// web-tree-sitter is pinned to 0.25.x: tree-sitter-wasms 0.1.13 grammars are
// ABI-incompatible with web-tree-sitter 0.26+/0.27 (dylink section layout).
import { Parser, Language, type Tree, type Node } from 'web-tree-sitter'

export type SymKind = 'function' | 'type' | 'class' | 'method' | 'var'

export interface Sym {
  name: string
  kind: SymKind
  line: number // 1-based
  col: number // 1-based
}

export const supportedExt = (path: string): boolean => {
  const base = path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1)
  const dot = base.lastIndexOf('.')
  if (dot <= 0) return false
  const ext = base.slice(dot + 1)
  return ['go', 'ts', 'tsx', 'js', 'jsx', 'mjs', 'cjs', 'php'].includes(ext)
}

// TS/TSX/JS grammars share the same node shapes; pick per extension.
type LangName = 'go' | 'typescript' | 'tsx' | 'javascript' | 'php'
const langForPath = (path: string): LangName => {
  const base = path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1)
  const dot = base.lastIndexOf('.')
  const ext = dot <= 0 ? '' : base.slice(dot + 1)
  if (ext === 'go') return 'go'
  if (ext === 'ts' || ext === 'mts' || ext === 'cts') return 'typescript'
  if (ext === 'tsx') return 'tsx'
  if (ext === 'php') return 'php'
  return 'javascript'
}

// Grammar sources are injectable so tests can load wasm bytes straight off
// disk instead of going through bundler asset imports. The app default uses
// dynamic `?url` imports (Vite emits them as hashed assets).
export type GrammarSource = () => Promise<string | Uint8Array>
const defaultSources: Record<LangName, GrammarSource> = {
  go: async () => (await import('tree-sitter-wasms/out/tree-sitter-go.wasm?url')).default,
  typescript: async () => (await import('tree-sitter-wasms/out/tree-sitter-typescript.wasm?url')).default,
  tsx: async () => (await import('tree-sitter-wasms/out/tree-sitter-tsx.wasm?url')).default,
  javascript: async () => (await import('tree-sitter-wasms/out/tree-sitter-javascript.wasm?url')).default,
  php: async () => (await import('tree-sitter-wasms/out/tree-sitter-php.wasm?url')).default,
}
let sources: Record<LangName, GrammarSource> | null = null
export const configureGrammars = (s: Partial<Record<LangName, GrammarSource>>): void => {
  sources = { ...defaultSources, ...s }
}

let parserReady: Promise<boolean> | null = null
const langs = new Map<LangName, Language>()

const initParser = (): Promise<boolean> => {
  if (!parserReady) {
    parserReady = (async () => {
      try {
        await Parser.init()
        const src = sources ?? defaultSources
        for (const name of ['go', 'typescript', 'tsx', 'javascript', 'php'] as LangName[]) {
          langs.set(name, await Language.load(await src[name]()))
        }
        return true
      } catch {
        parserReady = null
        return false
      }
    })()
  }
  return parserReady
}

let parser: Parser | null = null

export const parserStatus = (): Promise<boolean> => initParser()

// Go: kind mapping. const/const_spec and var/var_spec both map to 'var'
// (spec says kinds: function|type|class|method|var).
const extractGo = (tree: Tree, text: string): Sym[] => {
  const out: Sym[] = []
  const lineStarts: number[] = [0]
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '\n') lineStarts.push(i + 1)
  }
  const pos = (node: Node) => {
    const idx = node.startIndex
    let lo = 0
    let hi = lineStarts.length - 1
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1
      if (lineStarts[mid] <= idx) lo = mid
      else hi = mid - 1
    }
    return { line: lo + 1, col: idx - lineStarts[lo] + 1 }
  }
  const add = (node: Node, kind: SymKind, name: string) => {
    const { line, col } = pos(node)
    out.push({ name, kind, line, col })
  }
  const visit = (n: Node) => {
    switch (n.type) {
      case 'function_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'function', nameNode.text)
        break
      }
      case 'method_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'method', nameNode.text)
        break
      }
      case 'type_spec':
      case 'type_alias': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'type', nameNode.text)
        break
      }
      case 'const_spec':
      case 'var_spec': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'var', nameNode.text)
        break
      }
    }
    for (let i = 0; i < n.childCount; i++) {
      const c = n.child(i)
      if (c) visit(c)
    }
  }
  visit(tree.rootNode)
  return out
}

// TS/JS: top-level declarations plus class methods. Kinds: function, class,
// method, type (interface/type alias/enum), var (const/let).
const extractTs = (tree: Tree, text: string): Sym[] => {
  const out: Sym[] = []
  const lineStarts: number[] = [0]
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '\n') lineStarts.push(i + 1)
  }
  const pos = (node: Node) => {
    const idx = node.startIndex
    let lo = 0
    let hi = lineStarts.length - 1
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1
      if (lineStarts[mid] <= idx) lo = mid
      else hi = mid - 1
    }
    return { line: lo + 1, col: idx - lineStarts[lo] + 1 }
  }
  const add = (node: Node, kind: SymKind, name: string) => {
    const { line, col } = pos(node)
    out.push({ name, kind, line, col })
  }
  const visit = (n: Node, depth: number) => {
    switch (n.type) {
      case 'function_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'function', nameNode.text)
        break
      }
      case 'generator_function_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'function', nameNode.text)
        break
      }
      case 'class_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'class', nameNode.text)
        // Recurse into class body for methods via generic traversal below:
        break
      }
      case 'method_definition': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'method', nameNode.text)
        break
      }
      case 'interface_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'type', nameNode.text)
        break
      }
      case 'type_alias_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'type', nameNode.text)
        break
      }
      case 'enum_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'type', nameNode.text)
        break
      }
      case 'lexical_declaration':
      case 'variable_declaration': {
        for (let i = 0; i < n.childCount; i++) {
          const c = n.child(i)
          if (c && c.type === 'variable_declarator') {
            const nameNode = c.childForFieldName('name')
            if (nameNode) add(nameNode, 'var', nameNode.text)
          }
        }
        return
      }
    }
    for (let i = 0; i < n.childCount; i++) {
      const c = n.child(i)
      if (c) visit(c, depth + 1)
    }
  }
  visit(tree.rootNode, 0)
  return out
}

// PHP: functions, classes, methods, interfaces/traits/enums as type,
// top-level const/property declarations as var.
const extractPhp = (tree: Tree, text: string): Sym[] => {
  const out: Sym[] = []
  const lineStarts: number[] = [0]
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '\n') lineStarts.push(i + 1)
  }
  const pos = (node: Node) => {
    const idx = node.startIndex
    let lo = 0
    let hi = lineStarts.length - 1
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1
      if (lineStarts[mid] <= idx) lo = mid
      else hi = mid - 1
    }
    return { line: lo + 1, col: idx - lineStarts[lo] + 1 }
  }
  const add = (node: Node, kind: SymKind, name: string) => {
    const { line, col } = pos(node)
    out.push({ name, kind, line, col })
  }
  const memberNameChild = (body: Node | null, types: string[]): Node | null => {
    if (!body) return null
    for (let i = 0; i < body.childCount; i++) {
      const c = body.child(i)
      if (c && types.includes(c.type)) return c
    }
    return null
  }
  const visit = (n: Node) => {
    switch (n.type) {
      case 'function_definition': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'function', nameNode.text)
        break
      }
      case 'class_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'class', nameNode.text)
        break
      }
      case 'interface_declaration':
      case 'trait_declaration':
      case 'enum_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'type', nameNode.text)
        break
      }
      case 'method_declaration': {
        const nameNode = n.childForFieldName('name')
        if (nameNode) add(nameNode, 'method', nameNode.text)
        break
      }
      case 'const_element': {
        const names = memberNameChild(n, ['name'])
        if (names) add(names, 'var', names.text)
        break
      }
      case 'property_element': {
        const vn = memberNameChild(n, ['variable_name'])
        const names = vn ? memberNameChild(vn, ['name']) : null
        if (names) add(names, 'var', names.text)
        break
      }
    }
    for (let i = 0; i < n.childCount; i++) {
      const c = n.child(i)
      if (c) visit(c)
    }
  }
  visit(tree.rootNode)
  return out
}

export const extractSymbolsSync = (path: string, text: string): Sym[] => {
  if (!parser) return []
  const lang = langs.get(langForPath(path))
  if (!lang) return []
  parser.setLanguage(lang)
  let tree: Tree | null = null
  try {
    tree = parser.parse(text)
  } catch {
    return []
  }
  if (!tree) return []
  let syms: Sym[]
  if (path.endsWith('.go')) syms = extractGo(tree, text)
  else if (path.endsWith('.php')) syms = extractPhp(tree, text)
  else syms = extractTs(tree, text)
  tree.delete()
  return syms
}

export const extractSymbols = async (path: string, text: string): Promise<Sym[]> => {
  const ok = await initParser()
  if (!ok) return []
  if (!parser) parser = new Parser()
  return extractSymbolsSync(path, text)
}

export const disposeParser = (): void => {
  parser = null
  parserReady = null
  langs.clear()
}

// --- query scoring (pure, no parser) ---

// Lower rank = more prominent in results.
export const kindRank = (kind: SymKind): number => {
  switch (kind) {
    case 'class':
      return 0
    case 'type':
      return 1
    case 'function':
      return 2
    case 'method':
      return 3
    case 'var':
      return 4
  }
}

// scoreSymbol: -1 = no match. Non-negative = match score (higher is better).
// Startswith bonus over substring; kind-weighted.
export const scoreSymbol = (sym: Sym, term: string): number => {
  const name = sym.name
  const t = term.trim().toLowerCase()
  if (t === '') return 10 - kindRank(sym.kind)
  const n = name.toLowerCase()
  const idx = n.indexOf(t)
  if (idx === -1) return -1
  let score = 10 - kindRank(sym.kind)
  if (idx === 0) score += 20
  else score += 5
  // shorter names slightly preferred
  score += Math.max(0, 10 - name.length)
  return score
}
