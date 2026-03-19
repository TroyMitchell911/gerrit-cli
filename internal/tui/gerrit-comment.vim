" gerrit-comment.vim - Gerrit inline comment plugin for gerry
" Supports vim (9.0+) and nvim
" Variables expected to be set before sourcing:
"   g:gerrit_comment_key  - keybinding (e.g. 'gc' or 'c')
"   g:gerrit_comment_file - path to output JSON file

" ============================================================
" Initialization
" ============================================================

if has('nvim')
  let s:ns_id = nvim_create_namespace('gerrit_comments')
endif

let g:gerrit_comments = []

" Highlight group
highlight default GerritComment ctermfg=214 ctermbg=NONE guifg=#FFB86C gui=bold cterm=bold
highlight default GerritCommentSign ctermfg=214 guifg=#FFB86C

" Text property type for vim
if !has('nvim') && exists('*prop_type_add')
  silent! call prop_type_add('GerritComment', {
    \ 'highlight': 'GerritComment',
    \ 'override': 1
    \ })
endif

" ============================================================
" Diff parsing helpers
" ============================================================

function! s:GetFilePath()
  let i = line('.')
  while i >= 1
    let l = getline(i)
    if l =~# '^+++ '
      return substitute(l, '^+++ \(b/\)\?', '', '')
    endif
    let i -= 1
  endwhile
  return 'unknown'
endfunction

function! s:GetLineInfo()
  let cur = line('.')
  let cur_content = getline(cur)
  let side = cur_content =~# '^-' ? 'PARENT' : 'REVISION'

  " Find nearest hunk header above cursor
  let i = cur
  while i >= 1
    let l = getline(i)
    if l =~# '^@@'
      " Parse @@ -old_start[,count] +new_start[,count] @@
      let old_m = matchlist(l, '-\(\d\+\)')
      let new_m = matchlist(l, '+\(\d\+\)')
      let old_start = len(old_m) >= 2 ? str2nr(old_m[1]) : 1
      let new_start = len(new_m) >= 2 ? str2nr(new_m[1]) : 1

      " Walk from hunk header to cursor counting lines
      let old_line = old_start
      let new_line = new_start
      let j = i + 1
      while j < cur
        let cl = getline(j)
        if cl =~# '^-'
          let old_line += 1
        elseif cl =~# '^+'
          let new_line += 1
        elseif cl !~# '^@@' && cl !~# '^---' && cl !~# '^+++'
          let old_line += 1
          let new_line += 1
        endif
        let j += 1
      endwhile

      let file_line = side ==# 'PARENT' ? old_line : new_line
      return {'line': file_line, 'side': side}
    endif
    let i -= 1
  endwhile

  return {'line': 0, 'side': side}
endfunction

" ============================================================
" Rendering
" ============================================================

function! s:RenderComment(lnum, text)
  let display = '  ▶ ' . a:text
  if has('nvim')
    call nvim_buf_set_extmark(0, s:ns_id, a:lnum - 1, 0, {
      \ 'virt_lines': [[[display, 'GerritComment']]],
      \ 'virt_lines_above': 0
      \ })
  elseif exists('*prop_add')
    " vim 9.0+ supports text below line via prop_add
    try
      call prop_add(a:lnum, 0, {
        \ 'type': 'GerritComment',
        \ 'text': display,
        \ 'text_align': 'below'
        \ })
    catch
      " Older vim fallback: sign in gutter
      execute 'sign define GerritComment' . a:lnum . ' text=▶ texthl=GerritCommentSign'
      execute 'sign place ' . (1000 + a:lnum) . ' line=' . a:lnum . ' name=GerritComment' . a:lnum . ' buffer=' . bufnr('%')
      echohl GerritComment
      echom '▶ [line ' . a:lnum . '] ' . a:text
      echohl None
    endtry
  else
    echohl GerritComment
    echom '▶ [line ' . a:lnum . '] ' . a:text
    echohl None
  endif
endfunction

" ============================================================
" JSON saving
" ============================================================

function! s:SaveComments()
  let lines = ['[']
  let total = len(g:gerrit_comments)
  let i = 0
  for c in g:gerrit_comments
    let comma = (i < total - 1) ? ',' : ''
    call add(lines, '  {')
    call add(lines, '    "path": "' . escape(c.path, '"\\') . '",')
    call add(lines, '    "line": ' . c.line . ',')
    call add(lines, '    "side": "' . c.side . '",')
    call add(lines, '    "message": "' . escape(c.message, '"\\') . '",')
    call add(lines, '    "diff_line": ' . c.diff_line)
    call add(lines, '  }' . comma)
    let i += 1
  endfor
  call add(lines, ']')
  call writefile(lines, g:gerrit_comment_file)
endfunction

" ============================================================
" Add comment
" ============================================================

function! s:AddComment()
  let cur = line('.')
  let content = getline(cur)

  if content =~# '^---\|^+++\|^@@\|^diff\|^index'
    echohl WarningMsg | echo 'Cannot comment on diff header line' | echohl None
    return
  endif

  let path = s:GetFilePath()
  let info = s:GetLineInfo()

  call inputsave()
  let msg = input('Comment [' . path . ':' . info.line . ']: ')
  call inputrestore()
  redraw

  if empty(msg)
    echo 'Cancelled'
    return
  endif

  call add(g:gerrit_comments, {
    \ 'path': path,
    \ 'line': info.line,
    \ 'side': info.side,
    \ 'message': msg,
    \ 'diff_line': cur
    \ })

  call s:RenderComment(cur, msg)
  call s:SaveComments()

  echo 'Comment saved (' . len(g:gerrit_comments) . ' total) → ' . g:gerrit_comment_file
endfunction

" ============================================================
" Edit comment
" ============================================================

function! s:EditComment()
  let cur = line('.')

  " Find comment at or near current line
  let idx = -1
  let i = 0
  for c in g:gerrit_comments
    if c.diff_line == cur
      let idx = i
      break
    endif
    let i += 1
  endfor

  if idx == -1
    " Try to find nearest comment within 3 lines
    let i = 0
    for c in g:gerrit_comments
      if abs(c.diff_line - cur) <= 3
        let idx = i
        break
      endif
      let i += 1
    endfor
  endif

  if idx == -1
    echohl WarningMsg | echo 'No comment found near this line. Use ge to edit, gd to delete.' | echohl None
    return
  endif

  let old_msg = g:gerrit_comments[idx].message
  call inputsave()
  let new_msg = input('Edit comment: ', old_msg)
  call inputrestore()
  redraw

  if empty(new_msg)
    echo 'Cancelled'
    return
  endif

  let g:gerrit_comments[idx].message = new_msg
  call s:RedrawAllComments()
  call s:SaveComments()

  echo 'Comment updated → ' . g:gerrit_comment_file
endfunction

" ============================================================
" Delete comment
" ============================================================

function! s:DeleteComment()
  let cur = line('.')

  let idx = -1
  let i = 0
  for c in g:gerrit_comments
    if c.diff_line == cur
      let idx = i
      break
    endif
    let i += 1
  endfor

  if idx == -1
    let i = 0
    for c in g:gerrit_comments
      if abs(c.diff_line - cur) <= 3
        let idx = i
        break
      endif
      let i += 1
    endfor
  endif

  if idx == -1
    echohl WarningMsg | echo 'No comment found near this line.' | echohl None
    return
  endif

  let preview = g:gerrit_comments[idx].message
  call inputsave()
  let confirm = input('Delete comment "' . preview[:40] . '"? [y/N]: ')
  call inputrestore()
  redraw

  if confirm !=# 'y' && confirm !=# 'Y'
    echo 'Cancelled'
    return
  endif

  call remove(g:gerrit_comments, idx)
  call s:RedrawAllComments()
  call s:SaveComments()

  echo 'Comment deleted (' . len(g:gerrit_comments) . ' remaining)'
endfunction

" ============================================================
" Redraw all comments (after edit/delete)
" ============================================================

function! s:RedrawAllComments()
  if has('nvim')
    call nvim_buf_clear_namespace(0, s:ns_id, 0, -1)
  elseif exists('*prop_remove')
    call prop_remove({'type': 'GerritComment', 'all': 1})
  endif

  for c in g:gerrit_comments
    call s:RenderComment(c.diff_line, c.message)
  endfor
endfunction

" ============================================================
" Setup
" ============================================================

execute 'nnoremap <buffer> <silent> ' . g:gerrit_comment_key . ' :call <SID>AddComment()<CR>'
execute 'nnoremap <buffer> <silent> ' . g:gerrit_edit_key . ' :call <SID>EditComment()<CR>'
execute 'nnoremap <buffer> <silent> ' . g:gerrit_delete_key . ' :call <SID>DeleteComment()<CR>'

" Intercept :wq - diff is read-only, comments are auto-saved
autocmd BufWriteCmd <buffer> echohl WarningMsg | echo "Read-only diff. Use :q to exit (comments auto-saved with " . g:gerrit_comment_key . ")" | echohl None

setlocal nomodifiable
setlocal readonly
setlocal noswapfile

" Show usage hint
redraw
echo g:gerrit_comment_key . ": add comment | :q: exit (auto-submit on quit)"
