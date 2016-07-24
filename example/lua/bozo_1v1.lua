local players = {}

local function playerList()
	local list = {}
	for playerName, playerData in pairs(players) do
		table.insert(list, playerData)
	end
	return list
end

-- TODO: richer player data structure. perhaps store 'players' outside the lua?
function queue.PlayerJoined(playerName)
	print("I now see a player to match! ", playerName, " ", queue.Title())
	players[playerName] = {
		name = playerName,
		skillLevel = 9001
	}

	local playerList = playerList()
	if #playerList == 2 then
		local maps = queue.ListMaps()
		local games = queue.ListGames()

		queue.NewMatch({
			map = maps[1],
			game = games[1],
			engineVersion = "99",
			players = {
				{
					name = playerList[1].name,
					ally = 0,
					team = 0,
				},
				{
					name = playerList[2].name,
					ally = 1,
					team = 1,
				}
			}
		})

		players[playerList[1]] = nil
		players[playerList[2]] = nil
	end
end

function queue.PlayerLeft(playerName)
	players[playerName] = nil
	print("a player LEFT!!!!", playerName, " ", queue.Title())
end

