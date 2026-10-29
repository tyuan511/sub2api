import overview from './overview'
import channels from './channels'
import upstreams from './upstreams'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import promptAudit from './promptAudit'
import plugins from './plugins'
import refunds from './refunds'

export default {
  ...overview,
  ...channels,
  ...upstreams,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...plugins,
  ...refunds,
}
