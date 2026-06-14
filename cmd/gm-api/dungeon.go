package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type generateDungeonReq struct {
	Theme string `json:"theme"`
	Size  int    `json:"size"`
}

type DungeonRoom struct {
	X           int      `json:"x"`
	Y           int      `json:"y"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Exits       []string `json:"exits"` // "north", "south", "east", "west"
}

type DungeonMap struct {
	Rooms []DungeonRoom `json:"rooms"`
}

var redisClient *redis.Client

func initRedis(url string) {
	if url == "" {
		url = "redis:6379"
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr: url,
	})
	// Quick ping to check if redis is up
	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		// Just nil it out if it fails, so we can fall back to no-cache
		redisClient = nil
	}
}

func (s *server) generateDungeon(w http.ResponseWriter, r *http.Request) {
	var req generateDungeonReq
	if !decode(w, r, &req) {
		return
	}
	if req.Theme == "" {
		req.Theme = "Ancient Ruin"
	}
	if req.Size <= 0 || req.Size > 10 {
		req.Size = 4
	}

	// Check cache
	ctx := context.Background()
	cacheKey := fmt.Sprintf("dungeon:%s:%d", req.Theme, req.Size)
	if redisClient != nil {
		if cached, err := redisClient.Get(ctx, cacheKey).Result(); err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(cached))
			return
		}
	}

	// Generate Layout (WFC-lite / procedural)
	rooms := generateLayout(req.Size)

	// Call Gemini for descriptions
	llmRooms, err := seedDescriptionsWithLLM(s.geminiKey, s.geminiModel, req.Theme, rooms)
	if err != nil {
		// Fallback to simple descriptions
		llmRooms = rooms
		for i := range llmRooms {
			llmRooms[i].Description = "A dark and mysterious room."
		}
	}

	respMap := DungeonMap{Rooms: llmRooms}
	raw, _ := json.Marshal(respMap)

	if redisClient != nil {
		redisClient.Set(ctx, cacheKey, raw, time.Hour)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

func generateLayout(size int) []DungeonRoom {
	grid := make([][]string, size)
	for i := range grid {
		grid[i] = make([]string, size)
	}

	var rooms []DungeonRoom

	startX, startY := size/2, size/2
	grid[startX][startY] = "Start"

	queue := [][2]int{{startX, startY}}
	visited := map[string]bool{fmt.Sprintf("%d,%d", startX, startY): true}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} // N, S, W, E
	dirNames := []string{"north", "south", "west", "east"}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		x, y := curr[0], curr[1]

		for _, d := range dirs {
			nx, ny := x+d[0], y+d[1]
			if nx >= 0 && nx < size && ny >= 0 && ny < size {
				key := fmt.Sprintf("%d,%d", nx, ny)
				if !visited[key] {
					if r.Float32() < 0.4 {
						visited[key] = true
						roomType := "Room"
						if r.Float32() < 0.2 {
							roomType = "Corridor"
						}
						grid[nx][ny] = roomType
						queue = append(queue, [2]int{nx, ny})
					}
				}
			}
		}
	}

	var farthest [2]int
	maxDist := 0
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			if grid[x][y] != "" && grid[x][y] != "Start" {
				dist := abs(x-startX) + abs(y-startY)
				if dist > maxDist {
					maxDist = dist
					farthest = [2]int{x, y}
				}
			}
		}
	}
	if maxDist > 0 {
		grid[farthest[0]][farthest[1]] = "Boss"
	}

	rooms = []DungeonRoom{}
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			if grid[x][y] != "" {
				room := DungeonRoom{X: x, Y: y, Type: grid[x][y]}
				for i, d := range dirs {
					nx, ny := x+d[0], y+d[1]
					if nx >= 0 && nx < size && ny >= 0 && ny < size && grid[nx][ny] != "" {
						room.Exits = append(room.Exits, dirNames[i])
					}
				}
				rooms = append(rooms, room)
			}
		}
	}

	return rooms
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func seedDescriptionsWithLLM(geminiKey, model, theme string, rooms []DungeonRoom) ([]DungeonRoom, error) {
	if geminiKey == "" {
		return nil, fmt.Errorf("no gemini key")
	}

	type RoomDesc struct {
		X           int    `json:"x"`
		Y           int    `json:"y"`
		Description string `json:"description"`
	}

	layoutJSON, _ := json.Marshal(rooms)

	systemPrompt := `You are an AI dungeon master. Given a JSON layout of a dungeon and a theme, output a JSON array of objects with x, y, and a rich 1-2 sentence description for each room. Only output JSON matching this array structure: [{"x": 0, "y": 0, "description": "..."}]. Do not output markdown.`

	userPrompt := fmt.Sprintf("Theme: %s\nLayout: %s", theme, string(layoutJSON))

	gReq := geminiRequest{}
	gReq.Contents = append(gReq.Contents, struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: userPrompt}},
	})

	gReq.SystemInstruction = &struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: systemPrompt}},
	}

	body, _ := json.Marshal(gReq)
	url := geminiURL(model, geminiKey)

	resp, err := geminiHTTP.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var gResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
		return nil, err
	}

	if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	text := strings.TrimSpace(gResp.Candidates[0].Content.Parts[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var descs []RoomDesc
	if err := json.Unmarshal([]byte(text), &descs); err != nil {
		return nil, err
	}

	for i, r := range rooms {
		for _, d := range descs {
			if r.X == d.X && r.Y == d.Y {
				rooms[i].Description = d.Description
				break
			}
		}
	}

	return rooms, nil
}
