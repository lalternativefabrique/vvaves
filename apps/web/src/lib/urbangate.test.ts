import { describe, expect, it } from 'vitest'
import { basicAuth, tokenRequest } from './urbangate'

describe('the provisioner token request', () => {
  it('authenticates the client the way Hydra expects it, in the header', () => {
    expect(basicAuth('vvaves-provisioner', 's3cret')).toBe(
      `Basic ${Buffer.from('vvaves-provisioner:s3cret').toString('base64')}`,
    )
  })

  it('names urbangate as audience, or the machine API refuses the token', () => {
    const body = tokenRequest('urbangate:keys:issue')
    expect(body.get('grant_type')).toBe('client_credentials')
    expect(body.get('scope')).toBe('urbangate:keys:issue')
    expect(body.get('audience')).toBe('urbangate')
    expect(body.has('client_secret')).toBe(false)
  })
})
