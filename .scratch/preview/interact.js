// Opens the delete dialog on the settings page, reads it, types the wrong code,
// then the right one, and reports what the button does at each step.
const PORT = 9340

async function main() {
  const t = await (await fetch(`http://127.0.0.1:${PORT}/json/new?${encodeURIComponent('http://localhost:8111/research/R1/settings')}`, { method: 'PUT' })).json()
  const ws = new WebSocket(t.webSocketDebuggerUrl)
  let id = 0
  const pending = new Map()
  await new Promise((r) => (ws.onopen = r))
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data)
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id) }
  }
  const send = (method, params = {}) =>
    new Promise((res) => { const i = ++id; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })) })

  const evaluate = async (expr) => {
    const r = await send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true })
    if (r.result?.exceptionDetails) return 'EXCEPTION: ' + JSON.stringify(r.result.exceptionDetails)
    return r.result?.result?.value
  }

  await send('Runtime.enable')
  await send('Page.enable')
  await new Promise((r) => setTimeout(r, 4000))

  console.log('1. click the danger-zone Delete button')
  console.log('   clicked:', await evaluate(`(() => {
    const b = [...document.querySelectorAll('.danger-row button')].find(x => x.textContent.trim() === 'Delete')
    if (!b) return 'button not found'
    b.click(); return true
  })()`))
  await new Promise((r) => setTimeout(r, 1500))

  console.log('2. dialog text:')
  console.log(await evaluate(`document.querySelector('.delete-research')?.innerText ?? 'DIALOG NOT OPEN'`))

  console.log('3. what holds focus:', await evaluate(`document.activeElement?.tagName + ' ' + (document.activeElement?.dataset?.confirmCode !== undefined ? '[data-confirm-code]' : document.activeElement?.className)`))

  console.log('4. Delete disabled before typing:', await evaluate(`
    [...document.querySelectorAll('.modal-actions button')].find(b => b.textContent.trim() === 'Delete')?.disabled`))

  const type = (v) => `(() => {
    const f = document.querySelector('[data-confirm-code]')
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set
    setter.call(f, ${JSON.stringify(v)})
    f.dispatchEvent(new Event('input', { bubbles: true }))
    return f.value
  })()`

  console.log('5. type a wrong code:', await evaluate(type('R9')))
  await new Promise((r) => setTimeout(r, 400))
  console.log('   Delete still disabled:', await evaluate(`
    [...document.querySelectorAll('.modal-actions button')].find(b => b.textContent.trim() === 'Delete')?.disabled`))

  console.log('6. type the right code, lower case and padded:', await evaluate(type('  r1 ')))
  await new Promise((r) => setTimeout(r, 400))
  console.log('   Delete enabled now:', await evaluate(`
    [...document.querySelectorAll('.modal-actions button')].find(b => b.textContent.trim() === 'Delete')?.disabled === false`))

  console.log('7. press Delete')
  await evaluate(`[...document.querySelectorAll('.modal-actions button')].find(b => b.textContent.trim() === 'Delete').click()`)
  await new Promise((r) => setTimeout(r, 3000))
  console.log('   url now:', await evaluate('location.pathname'))
  console.log('   toast:', await evaluate(`document.querySelector('.toast')?.innerText ?? '(none)'`))
  console.log('   list text:', await evaluate(`document.body.innerText.split('\\n').filter(l => /^R\\d/.test(l)).join(' | ')`))

  ws.close()
  await fetch(`http://127.0.0.1:${PORT}/json/close/${t.id}`)
}
main().catch((e) => console.log('FAILED: ' + e.message))
