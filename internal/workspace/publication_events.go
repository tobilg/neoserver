package workspace

// SubscribePublicationChanges provides coalesced wakeups for reconcilers. A
// notification is a hint; durable state remains the source of truth. Listeners
// never block publication or feature-write completion.
func (r *Registry) SubscribePublicationChanges() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	r.publicationListeners.Store(ch, struct{}{})
	return ch, func() { r.publicationListeners.Delete(ch) }
}
func (r *Registry) notifyPublicationChange() {
	r.publicationListeners.Range(func(key, value any) bool {
		select {
		case key.(chan struct{}) <- struct{}{}:
		default:
		}
		return true
	})
}
