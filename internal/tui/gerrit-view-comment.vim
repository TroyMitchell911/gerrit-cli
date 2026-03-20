" gerrit-view-comment.vim - Display Gerrit comments in file view
" Supports vim (9.0+) and nvim
" Variables expected to be set before sourcing:
"   g:gerrit_view_comment_file  - path to JSON file with comments
"   g:gerrit_view_target_line   - line number to jump to

" ============================================================
" Initialization
" ============================================================

if has('nvim')
  let s:ns_id = nvim_create_namespace('gerrit_view_comments')
endif

" Highlight groups (same as gerrit-comment.vim)
highlight default GerritComment ctermfg=214 ctermbg=NONE guifg=#FFB86C gui=bold cterm=bold
highlight default GerritCommentAuthor ctermfg=109 ctermbg=NONE guifg=#83A598 gui=bold cterm=bold

" Text property type for vim
if !has('nvim') && exists('*prop_type_add')
  silent! call prop_type_add('GerritViewComment', {
    \ 'highlight': 'GerritComment',
    \ 'override': 1
    \ })
endif

" ============================================================
" Rendering
" ============================================================

function! s:RenderComment(lnum, author, text)
  let header = '  ▶ ' . a:author . ':'
  let lines = split(a:text, "\n")

  if has('nvim')
    let virt_lines = [[[header, 'GerritCommentAuthor']]]
    for l in lines
      call add(virt_lines, [['    ' . l, 'GerritComment']])
    endfor
    call nvim_buf_set_extmark(0, s:ns_id, a:lnum - 1, 0, {
      \ 'virt_lines': virt_lines,
      \ 'virt_lines_above': 0
      \ })
  elseif exists('*prop_add')
    try
      let display = header . ' ' . join(lines, ' ')
      call prop_add(a:lnum, 0, {
        \ 'type': 'GerritViewComment',
        \ 'text': display,
        \ 'text_align': 'below'
        \ })
    catch
      execute 'sign define GerritVC' . a:lnum . ' text=▶ texthl=GerritComment'
      execute 'sign place ' . (1000 + a:lnum) . ' line=' . a:lnum . ' name=GerritVC' . a:lnum . ' buffer=' . bufnr('%')
      echohl GerritComment
      echom '▶ [line ' . a:lnum . '] ' . a:author . ': ' . join(lines, ' ')
      echohl None
    endtry
  else
    echohl GerritComment
    echom '▶ [line ' . a:lnum . '] ' . a:author . ': ' . join(lines, ' ')
    echohl None
  endif
endfunction

" ============================================================
" Load and render comments
" ============================================================

let s:comment_count = 0

if exists('g:gerrit_view_comment_file') && filereadable(g:gerrit_view_comment_file)
  let s:json_text = join(readfile(g:gerrit_view_comment_file), '')
  let s:comments = json_decode(s:json_text)
  let s:comment_count = len(s:comments)
  for c in s:comments
    if c.line > 0
      call s:RenderComment(c.line, c.author, c.message)
    endif
  endfor
endif

" ============================================================
" Jump to target line
" ============================================================

if exists('g:gerrit_view_target_line') && g:gerrit_view_target_line > 0
  execute g:gerrit_view_target_line
  normal! zz
endif

" ============================================================
" Setup
" ============================================================

setlocal nomodifiable
setlocal readonly
setlocal noswapfile

" Intercept :wq - file is read-only
autocmd BufWriteCmd <buffer> echohl WarningMsg | echo "Read-only file view. Use :q to exit." | echohl None

redraw
echo s:comment_count . ' comment(s) displayed | :q to exit'
