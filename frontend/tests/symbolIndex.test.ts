import { describe, it, expect, beforeAll } from 'vitest'
import fs from 'node:fs/promises'
import path from 'node:path'
import {
  configureGrammars,
  extractSymbols,
  extractSymbolsSync,
  supportedExt,
  kindRank,
  scoreSymbol,
  type Sym,
} from '../src/state/symbolIndex'

const wasmDir = path.resolve(import.meta.dirname, '../node_modules/tree-sitter-wasms/out')

beforeAll(() => {
  // Load grammar wasm bytes straight off disk; no bundler asset imports.
  configureGrammars({
    go: () => fs.readFile(path.join(wasmDir, 'tree-sitter-go.wasm')),
    typescript: () => fs.readFile(path.join(wasmDir, 'tree-sitter-typescript.wasm')),
    tsx: () => fs.readFile(path.join(wasmDir, 'tree-sitter-tsx.wasm')),
    javascript: () => fs.readFile(path.join(wasmDir, 'tree-sitter-javascript.wasm')),
    php: () => fs.readFile(path.join(wasmDir, 'tree-sitter-php.wasm')),
  })
})

const names = (syms: Sym[]) => syms.map((s) => `${s.kind}:${s.name}`)

describe('supportedExt', () => {
  it('accepts indexable extensions', () => {
    expect(supportedExt('a.go')).toBe(true)
    expect(supportedExt('a.ts')).toBe(true)
    expect(supportedExt('a.tsx')).toBe(true)
    expect(supportedExt('a.mjs')).toBe(true)
    expect(supportedExt('a')).toBe(false)
    expect(supportedExt('a.md')).toBe(false)
    expect(supportedExt('a.astro')).toBe(false)
    expect(supportedExt('a.css')).toBe(false)
    expect(supportedExt('a.test.ts')).toBe(true) // ext filter is by extension only
  })
})

describe('Go extraction', () => {
  let src: string
  beforeAll(() => {
    src = `package main

import "fmt"

// Hello says hi.
func Hello(a string) error {
	return nil
}

func (s *Server) Start(ctx context.Context) error { return nil }

type Server struct {
	port int
}

type Handler interface {
	Handle()
}

type Alias = string

type (
	A int
	B struct{}
)

const MaxRetries = 3

const (
	AConst = 1
	BConst = 2
)

var cache = map[string]int{}
`
  })

  it('extracts functions, methods, types, vars, consts with 1-based positions', async () => {
    const syms = await extractSymbols('main.go', src)
    expect(names(syms)).toEqual([
      'function:Hello',
      'method:Start',
      'type:Server',
      'type:Handler',
      'type:Alias',
      'type:A',
      'type:B',
      'var:MaxRetries',
      'var:AConst',
      'var:BConst',
      'var:cache',
    ])
    expect(syms[0].line).toBe(6)
    expect(syms[0].col).toBe(6)
    expect(syms[1].line).toBe(10)
    expect(syms[1].col).toBe(18)
    expect(syms[2].line).toBe(12)
    // kind 'const' is folded into 'var'
  })

  it('works in sync mode once initialized', async () => {
    await extractSymbols('main.go', 'func Warm() {}')
    const syms = extractSymbolsSync('main.go', src)
    expect(names(syms)).toEqual([
      'function:Hello',
      'method:Start',
      'type:Server',
      'type:Handler',
      'type:Alias',
      'type:A',
      'type:B',
      'var:MaxRetries',
      'var:AConst',
      'var:BConst',
      'var:cache',
    ])
  })

  it('tolerates syntax errors and extracts partial symbols', async () => {
    const syms = await extractSymbols('broken.go', 'func Good() {}\nfunc Bad( {')
    expect(names(syms)).toContain('function:Good')
  })
})

describe('TypeScript extraction', () => {
  const src = `export function hi() {}
function local() {}
export const arrow = () => 1
const plain = 2
export let mutable = 0
class Cls {
  constructor() {}
  method() {}
  static create() {}
}
interface Iface { x: number }
type TAlias = string
enum Color { Red }
export default function def() {}
export class ECls {}
export interface EIface {}
`

  it('extracts functions, consts, classes, interfaces, types, enums, methods', async () => {
    const syms = await extractSymbols('a.ts', src)
    expect(names(syms)).toEqual([
      'function:hi',
      'function:local',
      'var:arrow',
      'var:plain',
      'var:mutable',
      'class:Cls',
      'method:constructor',
      'method:method',
      'method:create',
      'type:Iface',
      'type:TAlias',
      'type:Color',
      'function:def',
      'class:ECls',
      'type:EIface',
    ])
    expect(syms[0].line).toBe(1)
    expect(syms[2].col).toBe(14)
  })

  it('handles tsx files with generics and JSX', async () => {
    const src = `export default function Widget() { return <div/> }
const Helper = ({ a }: { a: string }) => <b>{a}</b>
const n = compute<number>(x)
interface Props { a: number }
`
    const syms = await extractSymbols('a.tsx', src)
    expect(names(syms)).toEqual([
      'function:Widget',
      'var:Helper',
      'var:n',
      'type:Props',
    ])
  })

  it('handles javascript files', async () => {
    const src = `export function hi() {}
class Foo {}
const arrow = () => 2
module.exports = { hi }
`
    const syms = await extractSymbols('a.js', src)
    expect(names(syms)).toEqual(['function:hi', 'class:Foo', 'var:arrow'])
  })
})

describe('PHP extraction', () => {
  const src = `<?php

const MAX = 10;

function hi($name) {
    return "hi $name";
}

class Router {
    public $routes = [];

    private const KIND = 'router';

    public function add(string $r): void {}

    public static function make(): Router {}
}

interface Handler {
    public function handle();
}

trait CacheAware {
    public function warm() {}
}

enum Status: string {
    case Ok = 'ok';
}
`
  ;[0, 1].forEach((useSync) => {
    it('extracts functions, classes, methods, interfaces, traits, enums, consts', async () => {
      let syms: Sym[]
      if (useSync) {
        await extractSymbols('warm.php', "<?php function Warm() {}")
        syms = extractSymbolsSync('app.php', src)
      } else {
        syms = await extractSymbols('app.php', src)
      }
      expect(names(syms)).toEqual([
        'var:MAX',
        'function:hi',
        'class:Router',
        'var:routes',
        'var:KIND',
        'method:add',
        'method:make',
        'type:Handler',
        'method:handle',
        'type:CacheAware',
        'method:warm',
        'type:Status',
      ])
      expect(syms.find((s) => s.name === 'MAX')!.line).toBe(3)
      expect(syms.find((s) => s.name === 'hi')!.line).toBe(5)
      expect(syms.find((s) => s.name === 'Router')!.line).toBe(9)
    })
  })

  it('tolerates syntax errors and extracts partial symbols', async () => {
    const syms = await extractSymbols('broken.php', '<?php function Good() {}\nfunction bad( {')
    expect(names(syms)).toContain('function:Good')
  })

  it('rejects non-php extensions', () => {
    expect(supportedExt('a.php')).toBe(true)
    expect(supportedExt('a.hh')).toBe(false)
  })
})

describe('query scoring', () => {
  const sym = (name: string, kind: Sym['kind'] = 'function'): Sym => ({
    name,
    kind,
    line: 1,
    col: 1,
  })

  it('ranks kind-weighted: type/class > function > method > var', () => {
    expect(kindRank('class')).toBeLessThan(kindRank('type'))
    expect(kindRank('type')).toBeLessThan(kindRank('function'))
    expect(kindRank('function')).toBeLessThan(kindRank('method'))
    expect(kindRank('method')).toBeLessThan(kindRank('var'))
  })

  it('gives startswith bonus over substring', () => {
    const start = scoreSymbol(sym('Handler'), 'hand')
    const mid = scoreSymbol(sym('MyHandler'), 'hand')
    expect(start).toBeGreaterThan(mid)
  })

  it('returns -1 for non-matches', () => {
    expect(scoreSymbol(sym('Handler'), 'xyz')).toBe(-1)
  })

  it('empty term matches everything with base score', () => {
    expect(scoreSymbol(sym('Handler'), '')).toBeGreaterThanOrEqual(0)
  })

  it('is case-insensitive', () => {
    expect(scoreSymbol(sym('HelloWorld'), 'hellow')).toBeGreaterThan(0)
  })
})
