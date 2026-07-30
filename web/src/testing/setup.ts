import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

HTMLDialogElement.prototype.showModal = function showModal() {
  this.open = true
}

HTMLDialogElement.prototype.close = function close() {
  this.open = false
}

afterEach(() => {
  cleanup()
})
