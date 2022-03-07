package main

var _version string = "1.0.1"

var _help string = `sub <mark_ids> - 訂閱缺陷種類，以收到排程訊息。參數留空為訂閱全部
unsub <all | mark_ids> - 取消訂閱缺陷種類。參數留空為取消訂閱全部，參數all為刪除所有記錄
list - 顯示目前訂閱狀況
inspect <all | mark_ids> - 手動調閱資料。參數留空為調閱已訂閱的缺陷資料，參數all為調閱所有缺陷之資料
leave - 離開群聊或群組
getid - 獲取當前對話的ID，可利用於手動觸發
version - 顯示機器人版本

mark_ids格式為D開頭接兩位數字，批量操作可用空白分開。例如：D00 D11 D22`
