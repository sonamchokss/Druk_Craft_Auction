import { config } from './config'
import { mockApi } from './mockApi'
import { realApi } from './realApi'
export const api = config.useMock ? mockApi : realApi
export { config }
