package ws

import (
	"time"

	"github.com/dovod-app/app/internal/service"
)

// HubNotifier bridges service.EventNotifier to the WebSocket Hub.
type HubNotifier struct {
	hub *Hub
}

func NewHubNotifier(hub *Hub) *HubNotifier {
	return &HubNotifier{hub: hub}
}

func (n *HubNotifier) Notify(event service.Event) {
	n.hub.Broadcast(Event{
		Type:          event.Type,
		ResearchID:    event.ResearchID,
		EntityID:      event.EntityID,
		Entity:        event.Entity,
		ParentID:      event.ParentID,
		ParentCode:    event.ParentCode,
		ActorUserID:   event.ActorUserID,
		ActorClientID: event.ActorClientID,
		Reason:        event.Reason,
		Name:          event.Name,
		TargetUserID:  event.TargetUserID,
		ResearchCode:  event.ResearchCode,
		At:            time.Now().UnixMilli(),
	})
}
