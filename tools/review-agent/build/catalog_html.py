#!/usr/bin/env python3
"""Render catalog.md as an HTML fragment and splice it into report.html between the
<!--CATALOG--> markers. Rerun after catalog.py.

Usage: catalog_html.py"""
import html,re

from common import AGENT_DIR

src,page=AGENT_DIR/"catalog.md",AGENT_DIR/"report.html"; s=src.read_text()
def inline(t):
    t=html.escape(t,quote=False)
    t=re.sub(r"`([^`]+)`",r"<code>\1</code>",t)
    return re.sub(r"\*\*(.+?)\*\*",r"<b>\1</b>",t)
LABEL={"Rule.":"Rule","Also.":"Also","Why.":"Why","Probe.":"Probe","CLAUDE.md already says:":"CLAUDE.md"}
out=[]; npass=0
for sec in re.split(r"\n## ",s)[1:]:
    head,*rest=sec.split("\n### "); lines=head.strip().split("\n"); title=lines[0]
    lead=" ".join(l for l in lines[1:] if l.strip())
    rules=[]
    for r in rest:
        rl=r.strip().split("\n"); rtitle=rl[0]
        m=re.search(r"`([^`]+)` · n=(\d+) · PRs=(\d+)",r); key,n,p=m.group(1),m.group(2),m.group(3)
        rows=[]
        for l in rl:
            mm=re.match(r"- \*\*(.+?)\*\*\s*(.*)",l)
            if not mm: continue
            lab,txt=mm.group(1),mm.group(2)
            if lab.startswith("Seen in"):
                ref=re.sub(r"^Seen in ","",lab); rows.append(("Seen in",f"<blockquote>{inline(txt.strip('“”'))} <span class='meta'>{html.escape(ref)}</span></blockquote>"))
            else: rows.append((LABEL.get(lab,lab.rstrip('.:')),inline(txt.strip('“”') if lab.startswith("CLAUDE") else txt)))
        dl="".join(f"<dt>{a}</dt><dd>{b}</dd>" for a,b in rows)
        rules.append(f'<details class="crule" data-text="{html.escape((rtitle+" "+key+" "+r).lower())}"><summary><span class="rt">{inline(rtitle)}</span><span class="rn">n={n} · {p} PRs</span></summary><div class="rule"><span class="meta">{key}</span><dl>{dl}</dl></div></details>')
    nf=sum(int(x) for x in re.findall(r"· n=(\d+) ·",sec)); npass+=1
    out.append(f'<details class="cpass"><summary><span class="pt">{html.escape(title)}</span><span class="rn">{len(rules)} rules · {nf} findings</span></summary><p class="muted">{inline(lead)}</p><div class="crules">{"".join(rules)}</div></details>')
frag="\n".join(out)
p=open(page).read(); a,b="<!--CATALOG-->","<!--/CATALOG-->"
i,j=p.index(a)+len(a),p.index(b); open(page,"w").write(p[:i]+"\n"+frag+"\n"+p[j:])
print("passes",npass,"rules",frag.count('class="crule"'),"fragment KB",len(frag)//1024)
