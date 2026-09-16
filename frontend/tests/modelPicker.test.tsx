// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ list: vi.fn(), set: vi.fn() }))
vi.mock('../bindings/codesaber/backend/app', () => ({ ACPModels: api.list, ACPSetModel: api.set }))
import ModelPicker from '../src/components/ModelPicker'
let root: Root
const onBusy = vi.fn()
const catalog = { sessionId: 'actual-session', currentId: 'fast', options: [{ id: 'fast', name: 'Fast' }, { id: 'deep', name: 'Deep' }] }
beforeEach(() => {
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
 api.list.mockReset().mockResolvedValue(catalog); api.set.mockReset(); onBusy.mockReset()
 document.body.innerHTML='<div id="root"></div>'; root=createRoot(document.getElementById('root')!)
})
afterEach(async()=>{await act(async()=>root.unmount());vi.unstubAllGlobals()})
const select = () => document.querySelector('select')!
const choose = async () => act(async()=>{select().value='deep';select().dispatchEvent(new Event('change',{bubbles:true}))})
it('renders advertised models and applies the provider-confirmed selection to the current session',async()=>{
 api.set.mockResolvedValue({...catalog,currentId:'deep'})
 await act(async()=>root.render(<ModelPicker projectId="p" thinking={false} onBusy={onBusy}/>))
 expect(select().value).toBe('fast')
 await choose()
 expect(api.set).toHaveBeenCalledWith('p','actual-session','deep')
 expect(select().value).toBe('deep')
 expect(onBusy).toHaveBeenLastCalledWith(false)
})
it('keeps the confirmed model and surfaces a failed change',async()=>{
 api.set.mockRejectedValue(new Error('Model access unavailable'))
 await act(async()=>root.render(<ModelPicker projectId="p" thinking={false} onBusy={onBusy}/>))
 await choose()
 expect(select().value).toBe('fast')
 expect(document.querySelector('[role="alert"]')?.textContent).toContain('Model access unavailable')
})
it('disables model changes during a response',async()=>{
 await act(async()=>root.render(<ModelPicker projectId="p" thinking={false} onBusy={onBusy}/>))
 await act(async()=>root.render(<ModelPicker projectId="p" thinking={true} onBusy={onBusy}/>))
 expect(select().disabled).toBe(true)
 expect(api.set).not.toHaveBeenCalled()
})
it('ignores an old project response after switching to another session',async()=>{
 let complete!: (value: typeof catalog)=>void
 api.list.mockReturnValueOnce(new Promise(resolve=>{complete=resolve})).mockResolvedValue({...catalog,sessionId:'new',currentId:'deep'})
 await act(async()=>root.render(<ModelPicker key="old" projectId="old" thinking={false} onBusy={onBusy}/>))
 await act(async()=>root.render(<ModelPicker key="new" projectId="new" thinking={false} onBusy={onBusy}/>))
 await act(async()=>complete(catalog))
 expect(select().value).toBe('deep')
})
it('explains when a provider offers no models',async()=>{
 api.list.mockResolvedValue({sessionId:'s',currentId:'',options:[]})
 await act(async()=>root.render(<ModelPicker projectId="p" thinking={false} onBusy={onBusy}/>))
 expect(select().disabled).toBe(true)
 expect(document.body.textContent).toContain('does not offer model selection')
})
it('distinguishes Go and Zen billing for the same model without changing provider IDs',async()=>{
 const go='opencode-go/shared-model', zen='opencode/shared-model'
 const state={sessionId:'s',currentId:go,options:[{id:go,name:'OpenCode Go / Shared'},{id:zen,name:'OpenCode Zen / Shared'}]}
 api.list.mockResolvedValue(state); api.set.mockResolvedValue({...state,currentId:zen})
 await act(async()=>root.render(<ModelPicker projectId="p" provider="opencode" thinking={false} onBusy={onBusy}/>))
 expect(document.body.textContent).toContain('OpenCode Go · subscription')
 await act(async()=>{select().value=zen;select().dispatchEvent(new Event('change',{bubbles:true}))})
 expect(api.set).toHaveBeenCalledWith('p','s',zen)
 expect(document.body.textContent).toContain('OpenCode Zen · usage-based billing')
 expect(document.body.textContent).not.toContain('OpenCode Go · subscription')
})
