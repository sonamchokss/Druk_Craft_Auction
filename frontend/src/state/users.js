// Demo users (there is no login yet). Shared by the user switcher and the API
// adapter, which shows names instead of raw IDs like "A-1".
export const users = [
  { id: 'U-3', name: 'Tashi Choden', role: 'Buyer', initials: 'TC' },
  { id: 'U-7', name: 'Dorji Wangchuk', role: 'Buyer', initials: 'DW' },
  { id: 'U-9', name: 'Pema Lhamo', role: 'Buyer', initials: 'PL' },
  { id: 'A-1', name: 'Karma Artisans', role: 'Artisan', initials: 'KA' },
]

export const nameOf = (id) => users.find((user) => user.id === id)?.name || id

// Craft categories offered when listing and when filtering.
export const categories = ['Masks', 'Textiles', 'Woodwork', 'Thangkas']
