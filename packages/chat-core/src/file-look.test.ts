import { describe, expect, it } from 'vitest'

import { fileLook } from './file-look'

describe('fileLook', () => {
  it('tells text files apart by their extension', () => {
    expect(fileLook('研究方案.md', 'text/plain')).toBe('note')
    expect(fileLook('Prompt.TXT', 'text/plain')).toBe('note')
    expect(fileLook('README', 'text/plain')).toBe('note')
    expect(fileLook('prices.csv', 'text/plain')).toBe('data')
    expect(fileLook('config.yaml', 'text/plain')).toBe('config')
    expect(fileLook('.env', 'text/plain')).toBe('config')
    expect(fileLook('demo.html', 'text/plain')).toBe('web')
    expect(fileLook('main.go', 'text/plain')).toBe('code')
    expect(fileLook('Dockerfile', 'text/plain')).toBe('code')
  })

  it('goes by the type for anything that is not text', () => {
    expect(fileLook('scan.pdf', 'application/pdf')).toBe('pdf')
    expect(fileLook('photo.md', 'image/png')).toBe('image')
    expect(fileLook('pack.zip', 'application/zip')).toBe('file')
  })
})
