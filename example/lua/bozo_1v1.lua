local players = {}

-- TODO: richer player data structure. perhaps store 'players' outside the lua?
function queue.PlayerJoined(playerName)
	players[playerName] = {
		name = playerName,
		skillLevel = 9001
	}

end

function queue.PlayerLeft(playerName)
	players[playerName] = nil
	print("a player left ", playerName, " ", queue.GetTitle())
end

function queue.Update(n)
	local title = queue.GetTitle()
	if n % 5 == 0 then
		local playerList = queue.GetPlayerList()
		if #playerList == 2 then
			print(queue.GetTitle(), " omg two players to match ", n)
			local maps = queue.GetMapList()
			local games = queue.GetGameList()

			queue.NewMatch({
				map = maps[1],
				game = games[1],
				engineVersion = "99",
				players = {
					{
						name = playerList[1],
						ally = 0,
						team = 0,
					},
					{
						name = playerList[2],
						ally = 1,
						team = 1,
					}
				}
			})

			players[playerList[1]] = nil
			players[playerList[2]] = nil
		end
	end
end

